package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type rolloutOutput struct {
	Namespace string                     `json:"namespace" yaml:"namespace"`
	Name      string                     `json:"name" yaml:"name"`
	Target    string                     `json:"target" yaml:"target"`
	Report    *hpaanalysis.RolloutReport `json:"rolloutReport" yaml:"rolloutReport"`
}

func newRolloutCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "rollout NAME [NAME...]",
		Short:             "Diagnose rollout-related HPA risks including readiness, startup probes, and container metric mismatches",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollout(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runRollout(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, presetRollout)

	outputs := make([]rolloutOutput, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReportWithClient(ctx, &local, name, false, nil)
		if err != nil {
			writeErrorIfStructured(out, local.Output, err)
			return err
		}

		rolloutReport := buildRolloutReport(ctx, &local, &report.Analysis, name)
		outputs = append(outputs, rolloutOutput{
			Namespace: report.Analysis.Namespace,
			Name:      report.Analysis.Name,
			Target:    report.Analysis.Target,
			Report:    rolloutReport,
		})
	}

	value := any(outputs)
	if len(outputs) == 1 {
		value = outputs[0]
	}

	format, templateStr := outputSelection(outputConfig{
		output: local.Output, template: local.Template, outputTemplates: local.OutputTemplates,
	})

	return writeOutput(out, format, templateStr, value, func() error {
		theme := style.NewTheme(shouldColorize(local.Color, out))
		for i, o := range outputs {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteRolloutReportText(out, o.Report, theme); err != nil {
				return err
			}
		}
		return nil
	})
}

// buildRolloutReport assembles RolloutInput and runs the rollout analysis.
// Warnings discovered while gathering live state are appended to analysis.Warnings
// so they surface in the report rather than being silently dropped.
func buildRolloutReport(ctx context.Context, opts *options, analysis *hpaanalysis.Analysis, name string) *hpaanalysis.RolloutReport {
	// Best-effort client: a client-creation failure here is not fatal to the
	// overall status report; returning nil lets the caller skip the rollout
	// section and record a warning instead of aborting. Bypasses the standard
	// "failed to create Kubernetes client" wrapper for that reason.
	client, err := opts.NewClient()
	if err != nil {
		return nil
	}

	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return nil
	}

	input := assembleRolloutInput(ctx, client, hpa, analysis)
	return hpaanalysis.AnalyzeRollout(input)
}

// assembleRolloutInput gathers all observable signals for rollout analysis.
// Fetch failures are appended to analysis.Warnings so callers can see why a
// signal is missing instead of silently treating it as absent.
func assembleRolloutInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, analysis *hpaanalysis.Analysis) hpaanalysis.RolloutInput {
	input := hpaanalysis.RolloutInput{
		Namespace: hpa.Namespace,
		HPAName:   hpa.Name,
		Target:    analysis.Target,
	}

	ref := hpa.Spec.ScaleTargetRef

	// Fetch scale target info and pod template.
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, ref)
	if err != nil || info == nil {
		return input
	}

	input.DesiredReplicas = info.DesiredReplicas
	input.ReadyReplicas = info.ReadyReplicas

	// Extract probe info and container names from pod template.
	if info.PodTemplate != nil {
		input = enrichRolloutInputFromTemplate(input, info.PodTemplate)
	}

	// Extract HPA ContainerResource metric container names.
	input.HPAContainerMetrics = extractHPAContainerMetrics(hpa)

	// Fetch Deployment/StatefulSet rollout status and new ReplicaSet containers.
	switch ref.Kind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err == nil {
			input.RolloutInProgress = deploymentRolloutInProgress(deploy)
			input.UpdatedReplicas = deploy.Status.UpdatedReplicas
			input.NewReplicaSetContainerNames = extractNewReplicaSetContainers(ctx, client, hpa.Namespace, deploy)
		} else {
			// Surface the fetch failure so RolloutInProgress=0 isn't mistaken
			// for "no rollout in progress" on an RBAC-denied or transient error.
			analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("could not read Deployment %s/%s rollout status: %v", hpa.Namespace, ref.Name, err))
		}
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err == nil {
			input.RolloutInProgress = sts.Status.UpdatedReplicas < replicasOrDefault(sts.Spec.Replicas) ||
				sts.Status.ReadyReplicas < replicasOrDefault(sts.Spec.Replicas)
			input.UpdatedReplicas = sts.Status.UpdatedReplicas
			for _, c := range sts.Spec.Template.Spec.Containers {
				input.NewReplicaSetContainerNames = append(input.NewReplicaSetContainerNames, c.Name)
			}
		} else {
			analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("could not read StatefulSet %s/%s rollout status: %v", hpa.Namespace, ref.Name, err))
		}
	}

	// Fetch pod issues from the existing rollout diagnosis.
	if analysis.RolloutDiagnosis != nil {
		input.PodIssues = analysis.RolloutDiagnosis.PodIssues
	}

	return input
}

// enrichRolloutInputFromTemplate extracts probe and container info from pod template.
func enrichRolloutInputFromTemplate(input hpaanalysis.RolloutInput, tmpl *corev1.PodTemplateSpec) hpaanalysis.RolloutInput {
	for _, container := range tmpl.Spec.Containers {
		input.TemplateContainerNames = append(input.TemplateContainerNames, container.Name)
	}
	if len(tmpl.Spec.Containers) > 0 {
		main := tmpl.Spec.Containers[0]
		if main.ReadinessProbe != nil {
			input.HasReadinessProbe = true
			input.ReadinessInitialDelaySeconds = main.ReadinessProbe.InitialDelaySeconds
		}
		if main.StartupProbe != nil {
			input.HasStartupProbe = true
		}
	}
	return input
}

// extractHPAContainerMetrics extracts container names from HPA ContainerResource metrics.
func extractHPAContainerMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	var names []string
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ContainerResourceMetricSourceType && metric.ContainerResource != nil {
			names = append(names, metric.ContainerResource.Container)
		}
	}
	return names
}

// extractNewReplicaSetContainers fetches the new ReplicaSet for a Deployment
// and returns its container names.
func extractNewReplicaSetContainers(ctx context.Context, client *kube.Client, namespace string, deploy *appsv1.Deployment) []string {
	rsList, err := client.Interface.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deploy.Spec.Selector),
	})
	if err != nil {
		// Fallback: use the Deployment's own pod template containers.
		var names []string
		for _, c := range deploy.Spec.Template.Spec.Containers {
			names = append(names, c.Name)
		}
		return names
	}

	for i := range rsList.Items {
		rs := &rsList.Items[i]
		for _, ownerRef := range rs.OwnerReferences {
			if ownerRef.UID == deploy.UID && rs.Spec.Replicas != nil && *rs.Spec.Replicas > 0 {
				if _, ok := rs.Labels["deployment.kubernetes.io/revision"]; ok {
					var names []string
					for _, c := range rs.Spec.Template.Spec.Containers {
						names = append(names, c.Name)
					}
					return names
				}
			}
		}
	}

	// Fallback: use the Deployment's own pod template containers.
	var names []string
	for _, c := range deploy.Spec.Template.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}
