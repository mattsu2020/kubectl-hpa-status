package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
)

type blockerOutput struct {
	Namespace string          `json:"namespace" yaml:"namespace"`
	Name      string          `json:"name" yaml:"name"`
	Target    string          `json:"target" yaml:"target"`
	Report    *blocker.Report `json:"blockerReport" yaml:"blockerReport"`
}

func newBlockersCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "blockers NAME [NAME...]",
		Short:             "Diagnose why HPA scale-out is not producing ready pods",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBlockers(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runBlockers(ctx context.Context, out io.Writer, opts *options, names []string) error {
	// Keep command execution isolated from the shared root options. The
	// dedicated blocker path gathers only the observations it needs, so it
	// does not run the full status pipeline or record an unrelated health
	// history sample.
	local := copyOptions(opts)
	client, err := newClientOrDefault(&local)
	if err != nil {
		return writeErrorIfStructured(out, local.Output, err)
	}

	outputs, err := collectPerHPA(ctx, &local, names, func(ctx context.Context, name string) (blockerOutput, error) {
		hpa, err := fetchHPA(ctx, client, name)
		if err != nil {
			return blockerOutput{}, err
		}
		analysis := hpaanalysis.AnalyzeWithOptions(hpa, false, analysisOptions(local.HealthWeights, local.Debug))
		blockerReport := buildBlockerReportForStatusWithSnapshot(
			ctx,
			client,
			hpa,
			analysis.Target,
			observation.New(client.Interface, hpa),
		)

		return blockerOutput{
			Namespace: analysis.Namespace,
			Name:      analysis.Name,
			Target:    analysis.Target,
			Report:    blockerReport,
		}, nil
	})
	if err != nil {
		return writeErrorIfStructured(out, local.Output, err)
	}

	return renderPerHPA(out, &local, outputs, func(out io.Writer, o blockerOutput) error {
		theme := themeFor(local.Color, out)
		if err := hpaanalysis.WriteBlockerText(out, o.Report, theme); err != nil {
			return fmt.Errorf("write blockers report for %s/%s: %w", o.Namespace, o.Name, err)
		}
		return nil
	})
}

// buildBlockerReportForStatusWithSnapshot builds a BlockerReport within an
// existing buildStatusReport call, reusing the already-created client and the
// caller's observation snapshot.
func buildBlockerReportForStatusWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string, snapshot *observation.Snapshot) *blocker.Report {
	input, warnings := assembleBlockerInputWithSnapshot(ctx, client, hpa, snapshot)
	report := blocker.AnalyzeBlockers(input)
	report.Namespace = hpa.Namespace
	report.Name = hpa.Name
	report.Target = target
	report.Warnings = warnings

	return report
}

// assembleBlockerInputWithSnapshot gathers all observable signals for blocker
// analysis from the caller's observation snapshot, returning any collection
// warnings alongside the input.
func assembleBlockerInputWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, snapshot *observation.Snapshot) (blocker.Input, []string) {
	input := blocker.Input{
		Namespace:       hpa.Namespace,
		DesiredReplicas: hpa.Status.DesiredReplicas,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		MinReplicas:     replicasOrDefault(hpa.Spec.MinReplicas),
		MaxReplicas:     hpa.Spec.MaxReplicas,
		ScalingActive:   hpaanalysis.IsScalingActive(hpa),
	}
	var warnings []string

	// Resolve scale target info.
	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	target := snapshot.ScaleTarget(ctx)
	input.TargetObservation = blocker.ObservationStatus(target.State)
	switch target.State {
	case observation.StateUnavailable:
		warnings = append(warnings, fmt.Sprintf("scale target unavailable: %v", target.Err))
		input.PodObservation = blocker.ObservationUnavailable
	case observation.StateNotApplicable:
		warnings = append(warnings, fmt.Sprintf(
			"scale target readiness is not observable for %s/%s",
			hpa.Spec.ScaleTargetRef.Kind,
			hpa.Spec.ScaleTargetRef.Name,
		))
		input.PodObservation = blocker.ObservationNotApplicable
	}
	if target.Known() {
		info := target.Data
		input.TargetReadyReplicas = info.ReadyReplicas
		input.TargetDesiredReplicas = info.DesiredReplicas

		selector := info.SelectorStr
		if selector != "" {
			podInfos := snapshot.PodInfos(ctx)
			input.PodObservation = blocker.ObservationStatus(podInfos.State)
			if podInfos.Known() {
				input = enrichBlockerInputFromPods(input, podInfos.Data)
			} else if podInfos.State == observation.StateUnavailable {
				warnings = append(warnings, fmt.Sprintf("pods unavailable: %v", podInfos.Err))
			}
			pendingDetails := snapshot.PendingPods(ctx)
			if pendingDetails.Known() {
				input.PendingPods = kubeconv.ToBlockerPodInfos(pendingDetails.Data)
			}
			containerStatuses := snapshot.ContainerStatuses(ctx)
			if containerStatuses.Known() {
				input.ContainerStatuses = convertToBlockerContainerStatuses(containerStatuses.Data)
			}

			// Fetch events for the scale target and pods.
			objectNames := blockerEventObjectNames(hpa, podInfos.Data)
			events := kube.FetchRecentEventsForObjects(ctx, client.Interface, hpa.Namespace, objectNames, 20)
			input.FailedSchedulingEvents = extractFailedSchedulingMessages(events)
		} else {
			input.PodObservation = blocker.ObservationNotApplicable
			warnings = append(warnings, "scale target Pod selector is unavailable")
		}
	}

	// Fetch ResourceQuotas.
	quotaInfos, quotaErr := kube.FetchResourceQuotas(ctx, client.Interface, hpa.Namespace)
	if quotaErr != nil {
		warnings = append(warnings, fmt.Sprintf("resource quotas unavailable: %v", quotaErr))
	} else {
		input.Quotas = convertToBlockerQuotas(quotaInfos)
	}

	// Fetch node capacity (deep mode).
	nodeCap, nodeErr := kube.FetchNodeCapacity(ctx, client.Interface)
	if nodeErr != nil {
		warnings = append(warnings, fmt.Sprintf("node capacity unavailable: %v", nodeErr))
	}
	if nodeCap != nil {
		input.NodeCapacity = &blocker.NodeCapacitySummary{
			TotalNodes:            nodeCap.TotalNodes,
			SchedulableNodes:      nodeCap.SchedulableNodes,
			SchedulableNodesKnown: true,
			AllocCPU:              nodeCap.AllocCPU.String(),
			AllocMemory:           nodeCap.AllocMemory.String(),
			TaintedNodes:          nodeCap.TaintedNodes,
		}
	}

	return input, warnings
}

// enrichBlockerInputFromPods counts ready/total pods from PodInfo slice.
func enrichBlockerInputFromPods(input blocker.Input, pods []kube.PodInfo) blocker.Input {
	var ready, total int32
	for _, pod := range pods {
		total++
		if pod.Ready {
			ready++
		}
	}
	input.ReadyPods = ready
	input.TotalPods = total
	return input
}

// convertToBlockerContainerStatuses converts internal ContainerStatusDetail
// to ContainerStatusSummary.
func convertToBlockerContainerStatuses(details []kube.ContainerStatusDetail) []blocker.ContainerStatusSummary {
	if len(details) == 0 {
		return nil
	}
	result := make([]blocker.ContainerStatusSummary, 0, len(details))
	for _, d := range details {
		result = append(result, blocker.ContainerStatusSummary{
			Pod:           d.Pod,
			Container:     d.Container,
			Waiting:       d.Waiting,
			WaitingReason: d.WaitingReason,
			RestartCount:  d.RestartCount,
		})
	}
	return result
}

// convertToBlockerQuotas converts internal QuotaInfo to BlockerQuotaInfo,
// computing the usage ratio.
func convertToBlockerQuotas(infos []kube.QuotaInfo) []blocker.QuotaInfo {
	return kubeconv.QuotaDetail(infos, func(q kube.QuotaInfo) blocker.QuotaInfo {
		return blocker.QuotaInfo{
			Name:     q.Name,
			Resource: q.Resource,
			Used:     q.Used,
			Hard:     q.Hard,
			Ratio:    q.Ratio,
		}
	})
}

// extractFailedSchedulingMessages returns messages from events with reason
// FailedScheduling.
func extractFailedSchedulingMessages(events []kube.EventInfo) []string {
	var messages []string
	for _, e := range events {
		if e.Reason == "FailedScheduling" {
			messages = append(messages, e.Message)
		}
	}
	return messages
}

// blockerEventObjectNames collects object names for event fetching.
func blockerEventObjectNames(hpa *autoscalingv2.HorizontalPodAutoscaler, pods []kube.PodInfo) []string {
	names := []string{hpa.Name, hpa.Spec.ScaleTargetRef.Name}
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return names
}

// replicasOrDefault returns the value or 1 if nil.
func replicasOrDefault(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}
