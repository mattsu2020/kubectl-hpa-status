package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

type blockerOutput struct {
	Namespace string                     `json:"namespace" yaml:"namespace"`
	Name      string                     `json:"name" yaml:"name"`
	Target    string                     `json:"target" yaml:"target"`
	Report    *hpaanalysis.BlockerReport `json:"blockerReport" yaml:"blockerReport"`
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
	// Enable the data sources needed for blocker analysis. Take a shallow copy
	// so the shared process-wide opts is not mutated (reference fields like
	// clientOverride and outputTemplates are intentionally shared by value).
	local := copyOptions(opts)
	local.CapacityContext = true
	local.ExplainPods = true
	local.Events = EventOption{Enabled: true, Limit: 10}

	outputs := make([]blockerOutput, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReportWithClient(ctx, &local, name, false, nil)
		if err != nil {
			if local.Output == "json" || local.Output == "yaml" {
				writeError(out, local.Output, err)
			}
			return err
		}

		// Build the blocker report from assembled data.
		blockerReport := buildBlockerReport(ctx, &local, report.Analysis, report.Analysis.Namespace, name)
		report.Analysis.BlockerReport = blockerReport

		outputs = append(outputs, blockerOutput{
			Namespace: report.Analysis.Namespace,
			Name:      report.Analysis.Name,
			Target:    report.Analysis.Target,
			Report:    blockerReport,
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
					return fmt.Errorf("write blockers separator: %w", err)
				}
			}
			if err := hpaanalysis.WriteBlockerText(out, o.Report, theme); err != nil {
				return fmt.Errorf("write blockers report for %s/%s: %w", o.Namespace, o.Name, err)
			}
		}
		return nil
	})
}

// buildBlockerReport assembles BlockerInput from various fetchers and runs
// the blocker analysis engine.
func buildBlockerReport(ctx context.Context, opts *options, analysis hpaanalysis.Analysis, namespace, name string) *hpaanalysis.BlockerReport {
	// Best-effort client: a client-creation failure here is not fatal to the
	// overall status report; returning nil lets the caller skip the blocker
	// section and record a warning instead of aborting. Bypasses the standard
	// "failed to create Kubernetes client" wrapper for that reason.
	client, err := opts.NewClient()
	if err != nil {
		return nil
	}

	// Get the HPA object for the input.
	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return nil
	}

	input := assembleBlockerInput(ctx, client, hpa)
	report := hpaanalysis.AnalyzeBlockers(input)
	report.Namespace = namespace
	report.Name = name
	report.Target = analysis.Target

	return report
}

// buildBlockerReportForStatus builds a BlockerReport within an existing
// buildStatusReport call, reusing the already-created client.
func buildBlockerReportForStatus(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string) *hpaanalysis.BlockerReport {
	input := assembleBlockerInput(ctx, client, hpa)
	report := hpaanalysis.AnalyzeBlockers(input)
	report.Namespace = hpa.Namespace
	report.Name = hpa.Name
	report.Target = target

	return report
}

// assembleBlockerInput gathers all observable signals for blocker analysis.
func assembleBlockerInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) hpaanalysis.BlockerInput {
	input := hpaanalysis.BlockerInput{
		Namespace:       hpa.Namespace,
		DesiredReplicas: hpa.Status.DesiredReplicas,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		MinReplicas:     replicasOrDefault(hpa.Spec.MinReplicas),
		MaxReplicas:     hpa.Spec.MaxReplicas,
		ScalingActive:   hpaanalysis.IsScalingActive(hpa),
	}

	// Resolve scale target info.
	ref := hpa.Spec.ScaleTargetRef
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, ref)
	if err == nil && info != nil {
		input.TargetReadyReplicas = info.ReadyReplicas
		input.TargetDesiredReplicas = info.DesiredReplicas

		selector := info.SelectorStr
		if selector != "" {
			// Fetch pod-level details.
			podInfos, _ := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, selector)
			input = enrichBlockerInputFromPods(input, podInfos)

			// Fetch pending pod details.
			pendingDetails, _ := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
			input.PendingPods = convertToBlockerPodInfos(pendingDetails)

			// Fetch container statuses.
			containerStatuses, _ := kube.FetchContainerStatuses(ctx, client.Interface, hpa.Namespace, selector)
			input.ContainerStatuses = convertToBlockerContainerStatuses(containerStatuses)

			// Fetch events for the scale target and pods.
			objectNames := blockerEventObjectNames(hpa, podInfos)
			events := kube.FetchRecentEventsForObjects(ctx, client.Interface, hpa.Namespace, objectNames, 20)
			input.FailedSchedulingEvents = extractFailedSchedulingMessages(events)
		}
	}

	// Fetch ResourceQuotas.
	quotaInfos, _ := kube.FetchResourceQuotas(ctx, client.Interface, hpa.Namespace)
	input.Quotas = convertToBlockerQuotas(quotaInfos)

	// Fetch node capacity (deep mode).
	nodeCap, _ := kube.FetchNodeCapacity(ctx, client.Interface)
	if nodeCap != nil {
		input.NodeCapacity = &hpaanalysis.NodeCapacitySummary{
			TotalNodes:   nodeCap.TotalNodes,
			AllocCPU:     nodeCap.AllocCPU.String(),
			AllocMemory:  nodeCap.AllocMemory.String(),
			TaintedNodes: nodeCap.TaintedNodes,
		}
	}

	return input
}

// enrichBlockerInputFromPods counts ready/total pods from PodInfo slice.
func enrichBlockerInputFromPods(input hpaanalysis.BlockerInput, pods []kube.PodInfo) hpaanalysis.BlockerInput {
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
func convertToBlockerContainerStatuses(details []kube.ContainerStatusDetail) []hpaanalysis.ContainerStatusSummary {
	if len(details) == 0 {
		return nil
	}
	result := make([]hpaanalysis.ContainerStatusSummary, 0, len(details))
	for _, d := range details {
		result = append(result, hpaanalysis.ContainerStatusSummary{
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
func convertToBlockerQuotas(infos []kube.QuotaInfo) []hpaanalysis.BlockerQuotaInfo {
	return convertQuotaDetail(infos, func(q kube.QuotaInfo) hpaanalysis.BlockerQuotaInfo {
		return hpaanalysis.BlockerQuotaInfo{
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
