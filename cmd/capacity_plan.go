package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

type capacityPlanOutput struct {
	Namespace string                    `json:"namespace" yaml:"namespace"`
	Name      string                    `json:"name" yaml:"name"`
	Target    string                    `json:"target" yaml:"target"`
	Plan      *hpaanalysis.CapacityPlan `json:"capacityPlan" yaml:"capacityPlan"`
}

func newCapacityPlanCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "capacity NAME [NAME...]",
		Short:             "Diagnose whether it is safe to raise HPA maxReplicas",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapacityPlan(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runCapacityPlan(ctx context.Context, out io.Writer, opts *options, names []string) error {
	// Take a shallow copy so the shared process-wide opts is not mutated.
	local := copyOptions(opts)
	local.CheckResources = true
	local.CapacityContext = true
	local.CapacityDeep = true
	local.ExplainPods = true

	outputs := make([]capacityPlanOutput, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReportWithClient(ctx, opts, name, false, nil)
		if err != nil {
			writeErrorIfStructured(out, local.Output, err)
			return err
		}

		plan := buildCapacityPlan(ctx, opts, report.Analysis, name)
		report.Analysis.CapacityPlan = plan

		outputs = append(outputs, capacityPlanOutput{
			Namespace: report.Analysis.Namespace,
			Name:      report.Analysis.Name,
			Target:    report.Analysis.Target,
			Plan:      plan,
		})
	}

	value := any(outputs)
	if len(outputs) == 1 {
		value = outputs[0]
	}

	format, templateStr := selectOutputFromOptions(&local)

	return writeOutput(out, format, templateStr, value, func() error {
		theme := style.NewTheme(shouldColorize(local.Color, out))
		for i, o := range outputs {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return fmt.Errorf("write capacity separator: %w", err)
				}
			}
			if err := hpaanalysis.WriteCapacityPlanText(out, o.Plan, theme); err != nil {
				return fmt.Errorf("write capacity report for %s/%s: %w", o.Namespace, o.Name, err)
			}
		}
		return nil
	})
}

// buildCapacityPlan assembles CapacityPlanInput from various fetchers and runs
// the capacity plan analysis.
func buildCapacityPlan(ctx context.Context, opts *options, analysis hpaanalysis.Analysis, name string) *hpaanalysis.CapacityPlan {
	client, err := opts.NewClient()
	if err != nil {
		return hpaanalysis.AnalyzeCapacityPlan(capacityPlanInputFromAnalysis(analysis, opts.TargetMax,
			hpaanalysis.CapacityObservationError{Source: "Kubernetes client", Message: err.Error()}))
	}

	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return hpaanalysis.AnalyzeCapacityPlan(capacityPlanInputFromAnalysis(analysis, opts.TargetMax,
			hpaanalysis.CapacityObservationError{Source: "HPA", Message: err.Error()}))
	}

	input := assembleCapacityPlanInput(ctx, client, hpa, analysis, opts.TargetMax)
	return hpaanalysis.AnalyzeCapacityPlan(input)
}

func capacityPlanInputFromAnalysis(analysis hpaanalysis.Analysis, targetMax int32, observationErrors ...hpaanalysis.CapacityObservationError) hpaanalysis.CapacityPlanInput {
	return hpaanalysis.CapacityPlanInput{
		Namespace:         analysis.Namespace,
		HPAName:           analysis.Name,
		Target:            analysis.Target,
		CurrentReplicas:   analysis.Current,
		MaxReplicas:       analysis.Max,
		TargetMaxReplicas: targetMax,
		ObservationErrors: observationErrors,
	}
}

// buildCapacityPlanForStatus builds a CapacityPlan within an existing
// buildStatusReport call, reusing the already-created client.
func buildCapacityPlanForStatus(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string, targetMax int32) *hpaanalysis.CapacityPlan {
	analysis := hpaanalysis.Analysis{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Target:    target,
		Current:   hpa.Status.CurrentReplicas,
		Desired:   hpa.Status.DesiredReplicas,
		Max:       hpa.Spec.MaxReplicas,
	}
	input := assembleCapacityPlanInput(ctx, client, hpa, analysis, targetMax)
	return hpaanalysis.AnalyzeCapacityPlan(input)
}

// assembleCapacityPlanInput gathers all observable signals for capacity plan
// analysis.
func assembleCapacityPlanInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, analysis hpaanalysis.Analysis, targetMax int32) hpaanalysis.CapacityPlanInput {
	input := hpaanalysis.CapacityPlanInput{
		Namespace:         hpa.Namespace,
		HPAName:           hpa.Name,
		Target:            analysis.Target,
		CurrentReplicas:   hpa.Status.CurrentReplicas,
		MaxReplicas:       hpa.Spec.MaxReplicas,
		TargetMaxReplicas: targetMax,
	}
	collectScaleTargetCapacity(ctx, client, hpa, &input)
	collectCapacityQuotas(ctx, client, hpa.Namespace, &input)
	collectCapacityLimitRanges(ctx, client, hpa.Namespace, &input)
	collectCapacityClusterHeadroom(ctx, client, &input)
	collectCapacityPDBs(ctx, client, hpa, &input)
	collectCapacityAutoscaler(ctx, client, &input)
	return input
}

func addCapacityObservationError(input *hpaanalysis.CapacityPlanInput, source string, err error) {
	if err == nil {
		return
	}
	input.ObservationErrors = append(input.ObservationErrors, hpaanalysis.CapacityObservationError{
		Source:  source,
		Message: err.Error(),
	})
}

func collectScaleTargetCapacity(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, input *hpaanalysis.CapacityPlanInput) {
	ref := hpa.Spec.ScaleTargetRef
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, ref)
	switch {
	case err != nil:
		addCapacityObservationError(input, "scale target", err)
	case info == nil:
		addCapacityObservationError(input, "scale target", fmt.Errorf("resource information is unavailable for %s/%s", ref.Kind, ref.Name))
	default:
		collectScaleTargetResources(info, ref.Kind, ref.Name, input)
		collectScaleTargetPods(ctx, client, hpa.Namespace, info.SelectorStr, input)
	}
}

func collectScaleTargetResources(info *kube.ScaleTargetInfo, kind, name string, input *hpaanalysis.CapacityPlanInput) {
	resources := kube.ResourceRequestsFromPodTemplate(info.PodTemplate)
	if resources == nil {
		addCapacityObservationError(input, "Pod resource requests", fmt.Errorf("pod template is unavailable for %s/%s", kind, name))
		return
	}
	input.ContainerResources = convertToCapacityContainerResources(resources)
	input.PodRequestCPU = resources.PodRequests["cpu"]
	input.PodRequestMemory = resources.PodRequests["memory"]
}

func collectScaleTargetPods(ctx context.Context, client *kube.Client, namespace, selector string, input *hpaanalysis.CapacityPlanInput) {
	if selector == "" {
		addCapacityObservationError(input, "scale target Pod selector", fmt.Errorf("selector is empty"))
		return
	}
	podInfos, err := kube.FetchPodInfosForSelector(ctx, client.Interface, namespace, selector)
	if err != nil {
		addCapacityObservationError(input, "scale target Pods", err)
		return
	}
	for _, pod := range podInfos {
		if pod.Ready {
			input.ReadyPods++
		}
		if pod.Phase == "Pending" {
			input.PendingPods = append(input.PendingPods, hpaanalysis.PendingPodInfo{
				Name:          pod.Name,
				Phase:         pod.Phase,
				Unschedulable: pod.Unschedulable,
				Reasons:       pod.Reasons,
			})
		}
	}
}

func collectCapacityQuotas(ctx context.Context, client *kube.Client, namespace string, input *hpaanalysis.CapacityPlanInput) {
	quotaInfos, quotaErr := kube.FetchAllResourceQuotas(ctx, client.Interface, namespace)
	if quotaErr != nil {
		addCapacityObservationError(input, "ResourceQuotas", quotaErr)
	} else {
		input.Quotas = convertToCapacityQuotas(quotaInfos)
	}
}

func collectCapacityLimitRanges(ctx context.Context, client *kube.Client, namespace string, input *hpaanalysis.CapacityPlanInput) {
	lrInfos, limitRangeErr := kube.FetchLimitRanges(ctx, client.Interface, namespace)
	if limitRangeErr != nil {
		addCapacityObservationError(input, "LimitRanges", limitRangeErr)
	} else {
		input.LimitRanges = convertToCapacityLimitRanges(lrInfos)
	}
}

func collectCapacityClusterHeadroom(ctx context.Context, client *kube.Client, input *hpaanalysis.CapacityPlanInput) {
	clusterHeadroom, headroomErr := kube.FetchClusterResourceHeadroom(ctx, client.Interface)
	if headroomErr != nil {
		addCapacityObservationError(input, "cluster request headroom", headroomErr)
	} else if clusterHeadroom != nil && clusterHeadroom.NodeCapacity != nil {
		nodeCap := clusterHeadroom.NodeCapacity
		input.NodeCapacity = &blocker.NodeCapacitySummary{
			TotalNodes:      nodeCap.TotalNodes,
			AllocCPU:        nodeCap.AllocCPU.String(),
			AllocMemory:     nodeCap.AllocMemory.String(),
			RequestedCPU:    clusterHeadroom.RequestedCPU.String(),
			RequestedMemory: clusterHeadroom.RequestedMemory.String(),
			AvailableCPU:    clusterHeadroom.AvailableCPU.String(),
			AvailableMemory: clusterHeadroom.AvailableMemory.String(),
			TaintedNodes:    nodeCap.TaintedNodes,
		}
	}
}

func collectCapacityPDBs(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, input *hpaanalysis.CapacityPlanInput) {
	pdbInfos, pdbErr := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)
	if pdbErr != nil {
		addCapacityObservationError(input, "PodDisruptionBudgets", pdbErr)
	} else {
		input.PDBs = convertPDBsPlain(pdbInfos)
	}
}

func collectCapacityAutoscaler(ctx context.Context, client *kube.Client, input *hpaanalysis.CapacityPlanInput) {
	clusterAutoscaler, autoscalerErr := kube.DetectClusterAutoscalerWithError(ctx, client.Interface)
	if autoscalerErr != nil {
		addCapacityObservationError(input, "Cluster Autoscaler detection", autoscalerErr)
	} else {
		input.ClusterAutoscaler = clusterAutoscaler
	}
}

// ---------------------------------------------------------------------------
// Converter functions
// ---------------------------------------------------------------------------

func convertToCapacityContainerResources(rr *kube.ResourceRequests) []hpaanalysis.CapacityContainerResources {
	if rr == nil {
		return nil
	}
	result := make([]hpaanalysis.CapacityContainerResources, 0, len(rr.Containers)+len(rr.InitContainers))
	containers := append(append([]kube.ContainerResources(nil), rr.Containers...), rr.InitContainers...)
	for _, c := range containers {
		result = append(result, hpaanalysis.CapacityContainerResources{
			Name:   c.Name,
			CPU:    c.Requests["cpu"],
			Memory: c.Requests["memory"],
		})
	}
	return result
}

func convertToCapacityQuotas(infos []kube.QuotaInfo) []hpaanalysis.CapacityQuotaInfo {
	return convertQuotaDetail(infos, func(q kube.QuotaInfo) hpaanalysis.CapacityQuotaInfo {
		return hpaanalysis.CapacityQuotaInfo{
			Name:     q.Name,
			Resource: q.Resource,
			Used:     q.Used,
			Hard:     q.Hard,
		}
	})
}

func convertToCapacityLimitRanges(infos []kube.LimitRangeInfo) []hpaanalysis.LimitRangeConstraint {
	if len(infos) == 0 {
		return nil
	}
	result := make([]hpaanalysis.LimitRangeConstraint, 0, len(infos))
	for _, lr := range infos {
		result = append(result, hpaanalysis.LimitRangeConstraint{
			Name:     lr.Name,
			Type:     lr.Type,
			Resource: lr.Resource,
			Min:      lr.Min,
			Max:      lr.Max,
		})
	}
	return result
}
