package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	hpareadiness "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/readiness"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultCPUInitializationPeriod = 5 * time.Minute
)

func buildReadinessImpactWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, snapshot *observation.Snapshot) *hpareadiness.Impact {
	if client == nil || hpa == nil {
		return nil
	}
	profile := hpaanalysis.DefaultControllerProfile()
	impact := &hpareadiness.Impact{
		InitialReadinessDelay:   profile.InitialReadinessDelay,
		CPUInitializationPeriod: profile.CPUInitializationPeriod,
		NextChecks: []string{
			fmt.Sprintf("kubectl get pod -n %s -l <scale-target-selector>", hpa.Namespace),
			fmt.Sprintf("kubectl top pod -n %s -l <scale-target-selector>", hpa.Namespace),
		},
	}
	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	target := snapshot.ScaleTarget(ctx)
	if target.State == observation.StateUnavailable {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("scale target selector could not be resolved: %v", target.Err))
		return impact
	}
	if !target.Known() || target.Data.SelectorStr == "" {
		impact.Evidence = append(impact.Evidence, "scale target selector could not be resolved")
		return impact
	}
	info := target.Data
	impact.NextChecks = []string{
		fmt.Sprintf("kubectl get pod -n %s -l %q", hpa.Namespace, info.SelectorStr),
		fmt.Sprintf("kubectl top pod -n %s -l %q", hpa.Namespace, info.SelectorStr),
	}
	pods := snapshot.Pods(ctx)
	if pods.State == observation.StateUnavailable {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("failed to list pods: %v", pods.Err))
		return impact
	}
	if !pods.Known() {
		return impact
	}
	now := time.Now()
	impact.TotalPods = int32(len(pods.Data))
	countNotYetReadyPods(impact, pods.Data, hpa.Namespace, now)
	countMissingMetricPods(ctx, impact, client, hpa.Namespace, info.SelectorStr, pods.Data)
	finalizeReadinessImpact(impact)
	return impact
}

// countNotYetReadyPods increments NotYetReadyPods for young non-Ready pods and records evidence and describe-pod next-checks.
func countNotYetReadyPods(impact *hpareadiness.Impact, pods []corev1.Pod, namespace string, now time.Time) {
	for _, pod := range pods {
		if podReadyForImpact(pod) {
			continue
		}
		age := time.Duration(0)
		if pod.Status.StartTime != nil {
			age = now.Sub(pod.Status.StartTime.Time).Round(time.Second)
		}
		if age == 0 || age <= defaultCPUInitializationPeriod {
			impact.NotYetReadyPods++
			impact.Evidence = append(impact.Evidence, fmt.Sprintf("pod/%s: Ready=False, age=%s", pod.Name, age))
			if len(impact.NextChecks) < 4 {
				impact.NextChecks = append(impact.NextChecks, fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, namespace))
			}
		}
	}
}

// countMissingMetricPods increments MissingMetricPods for running pods lacking a PodMetrics sample, recording evidence.
func countMissingMetricPods(ctx context.Context, impact *hpareadiness.Impact, client *kube.Client, namespace, selector string, pods []corev1.Pod) {
	metricPods, metricErr := fetchPodMetricNames(ctx, client, namespace, selector)
	if metricErr != nil {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("PodMetrics not checked: %v", metricErr))
		return
	}
	seen := make(map[string]struct{}, len(metricPods))
	for _, name := range metricPods {
		seen[name] = struct{}{}
	}
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if _, ok := seen[pod.Name]; !ok {
			impact.MissingMetricPods++
			impact.Evidence = append(impact.Evidence, fmt.Sprintf("metrics window missing for pod/%s", pod.Name))
		}
	}
}

// finalizeReadinessImpact sets LikelyAffected and appends PossibleEffects based on the recorded counts.
func finalizeReadinessImpact(impact *hpareadiness.Impact) {
	impact.LikelyAffected = impact.NotYetReadyPods > 0 || impact.MissingMetricPods > 0
	if impact.NotYetReadyPods > 0 {
		impact.PossibleEffects = append(impact.PossibleEffects,
			fmt.Sprintf("scale-up may be dampened because %d pod(s) are still initializing", impact.NotYetReadyPods))
	}
	if impact.MissingMetricPods > 0 {
		impact.PossibleEffects = append(impact.PossibleEffects,
			fmt.Sprintf("scale direction may be conservative because %d pod(s) have no visible PodMetrics sample", impact.MissingMetricPods))
	}
	if impact.LikelyAffected {
		impact.PossibleEffects = append(impact.PossibleEffects,
			"HPA status.currentMetrics may not show the adjusted value used internally")
	}
}

func podReadyForImpact(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

type podMetricsNamesJSON struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"items"`
}

func fetchPodMetricNames(ctx context.Context, client *kube.Client, namespace, selector string) ([]string, error) {
	restClient := client.Interface.Discovery().RESTClient()
	if restClient == nil {
		return nil, fmt.Errorf("discovery REST client is unavailable")
	}
	raw, err := restClient.Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods").
		Param("labelSelector", selector).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var list podMetricsNamesJSON
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Metadata.Name != "" {
			names = append(names, item.Metadata.Name)
		}
	}
	return names, nil
}
