package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newReadinessDoctorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "doctor NAME",
		Short:             "Focused readiness diagnostic for HPA scale target pods",
		Long:              "Analyze pod age distribution, probe configuration, CPU initialization window impact, and metric exclusion estimates to diagnose why HPA may not be scaling as expected.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReadinessDoctor(cmd.Context(), cmd.OutOrStdout(), opts, args[0])
		},
	}
}

func runReadinessDoctor(ctx context.Context, out io.Writer, opts *options, name string) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return wrapHPALookupError(client.Namespace, name, err)
	}

	// Fetch scale target to get pod template (for probe info) and selector.
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil {
		return fmt.Errorf("failed to resolve scale target: %w", err)
	}
	if info == nil || info.SelectorStr == "" {
		return fmt.Errorf("could not resolve label selector for scale target")
	}

	// Extract probe configuration from pod template.
	hasStartupProbe := false
	hasReadinessProbe := false
	var readinessDelay int32
	var startupMaxDelay int32
	if info.PodTemplate != nil {
		for _, container := range info.PodTemplate.Spec.Containers {
			if container.ReadinessProbe != nil {
				hasReadinessProbe = true
				if container.ReadinessProbe.InitialDelaySeconds > readinessDelay {
					readinessDelay = container.ReadinessProbe.InitialDelaySeconds
				}
			}
			if container.StartupProbe != nil {
				hasStartupProbe = true
				maxDelay := container.StartupProbe.InitialDelaySeconds +
					container.StartupProbe.PeriodSeconds*(container.StartupProbe.FailureThreshold-1)
				if maxDelay > startupMaxDelay {
					startupMaxDelay = maxDelay
				}
			}
		}
	}

	// Fetch pods matching the selector.
	podList, err := client.Interface.CoreV1().Pods(hpa.Namespace).List(ctx,
		metav1.ListOptions{LabelSelector: info.SelectorStr})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Build pod details with age.
	podDetails := make([]hpaanalysis.ReadinessDoctorPod, 0, len(podList.Items))
	now := time.Now()
	for i := range podList.Items {
		pod := &podList.Items[i]
		ageSeconds := int64(0)
		if pod.Status.StartTime != nil {
			ageSeconds = int64(now.Sub(pod.Status.StartTime.Time).Seconds())
		}
		podDetails = append(podDetails, hpaanalysis.ReadinessDoctorPod{
			Name:       pod.Name,
			Ready:      isPodReady(pod),
			AgeSeconds: ageSeconds,
		})
	}

	// Count pods with missing metrics via PodMetrics API.
	missingMetrics := countMissingMetrics(ctx, client, hpa.Namespace, info.SelectorStr, podList.Items)

	target := fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	input := hpaanalysis.ReadinessDoctorInput{
		Namespace:             hpa.Namespace,
		HPAName:               hpa.Name,
		Target:                target,
		PodDetails:            podDetails,
		HasStartupProbe:       hasStartupProbe,
		HasReadinessProbe:     hasReadinessProbe,
		ReadinessInitialDelay: readinessDelay,
		StartupMaxDelay:       startupMaxDelay,
		CPUInitPeriodSeconds:  300, // default 5m
		InitialReadinessDelay: 30,  // default 30s
		MissingMetricPods:     missingMetrics,
	}

	report := hpaanalysis.AnalyzeReadinessDoctor(input)

	format, templateStr := outputSelection(outputConfig{
		output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates,
	})

	return writeOutput(out, format, templateStr, report, func() error {
		return hpaanalysis.WriteReadinessDoctorText(out, report,
			style.NewTheme(shouldColorize(opts.Color, out)))
	})
}

// isPodReady checks if a pod's Ready condition is True.
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// countMissingMetrics fetches PodMetrics and counts pods not present in the response.
func countMissingMetrics(ctx context.Context, client *kube.Client, namespace, selector string, pods []corev1.Pod) int32 {
	if client == nil || len(pods) == 0 {
		return 0
	}

	body, err := client.Interface.Discovery().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1").
		Namespace(namespace).
		Resource("pods").
		Param("labelSelector", selector).
		DoRaw(ctx)
	if err != nil {
		return 0
	}

	var metricsList struct {
		Items []struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &metricsList); err != nil {
		return 0
	}

	metricsMap := make(map[string]bool, len(metricsList.Items))
	for _, item := range metricsList.Items {
		metricsMap[item.Metadata.Name] = true
	}

	var missing int32
	for i := range pods {
		if !metricsMap[pods[i].Name] {
			missing++
		}
	}
	return missing
}
