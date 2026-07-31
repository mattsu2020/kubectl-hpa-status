package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
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
	// The dedicated capacity path gathers only capacity observations. Avoid
	// running the full status pipeline (and recording a health-history sample)
	// before fetching the same HPA and workload a second time.
	local := copyOptions(opts)
	client, err := newClientOrDefault(&local)
	if err != nil {
		writeErrorIfStructured(out, local.Output, err)
		return err
	}

	outputs := make([]capacityPlanOutput, 0, len(names))
	for _, name := range names {
		hpa, err := fetchHPA(ctx, client, name)
		if err != nil {
			writeErrorIfStructured(out, local.Output, err)
			return err
		}
		analysis := hpaanalysis.AnalyzeWithOptions(hpa, false, analysisOptions(local.HealthWeights, local.Debug))
		input := assembleCapacityPlanInputWithSnapshot(
			ctx,
			client,
			hpa,
			analysis,
			local.TargetMax,
			observation.New(client.Interface, hpa),
		)
		plan := hpaanalysis.AnalyzeCapacityPlan(input)

		outputs = append(outputs, capacityPlanOutput{
			Namespace: analysis.Namespace,
			Name:      analysis.Name,
			Target:    analysis.Target,
			Plan:      plan,
		})
	}

	value := any(outputs)
	if len(outputs) == 1 {
		value = outputs[0]
	}

	format, templateStr := selectOutputFromOptions(&local)

	return render.Format(out, format, templateStr, value, func(out io.Writer) error {
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

func buildCapacityPlanForStatusWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string, targetMax int32, snapshot *observation.Snapshot) *hpaanalysis.CapacityPlan {
	analysis := hpaanalysis.Analysis{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Target:    target,
		Current:   hpa.Status.CurrentReplicas,
		Desired:   hpa.Status.DesiredReplicas,
		Max:       hpa.Spec.MaxReplicas,
	}
	input := assembleCapacityPlanInputWithSnapshot(ctx, client, hpa, analysis, targetMax, snapshot)
	return hpaanalysis.AnalyzeCapacityPlan(input)
}

// assembleCapacityPlanInput gathers all observable signals for capacity plan
// analysis.
func assembleCapacityPlanInput(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, analysis hpaanalysis.Analysis, targetMax int32) hpaanalysis.CapacityPlanInput {
	return assembleCapacityPlanInputWithSnapshot(ctx, client, hpa, analysis, targetMax, observation.New(client.Interface, hpa))
}

func assembleCapacityPlanInputWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, analysis hpaanalysis.Analysis, targetMax int32, snapshot *observation.Snapshot) hpaanalysis.CapacityPlanInput {
	input := hpaanalysis.CapacityPlanInput{
		Namespace:         hpa.Namespace,
		HPAName:           hpa.Name,
		Target:            analysis.Target,
		CurrentReplicas:   hpa.Status.CurrentReplicas,
		MaxReplicas:       hpa.Spec.MaxReplicas,
		TargetMaxReplicas: targetMax,
	}
	podSpec := collectScaleTargetCapacity(ctx, client, hpa, snapshot, &input)
	collectCapacityQuotas(ctx, client, hpa.Namespace, &input)
	collectCapacityLimitRanges(ctx, client, hpa.Namespace, &input)
	collectCapacityClusterHeadroom(ctx, client, podSpec, &input)
	collectCapacityPDBs(ctx, client, hpa, &input)
	collectCapacityAutoscaler(ctx, client, &input)
	return input
}

func addCapacityObservationError(input *hpaanalysis.CapacityPlanInput, domain hpaanalysis.CapacityObservationDomain, source string, err error) {
	if err == nil {
		return
	}
	input.ObservationErrors = append(input.ObservationErrors, hpaanalysis.CapacityObservationError{
		Domain:  domain,
		Source:  source,
		Message: err.Error(),
	})
}

func collectScaleTargetCapacity(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, snapshot *observation.Snapshot, input *hpaanalysis.CapacityPlanInput) *corev1.PodSpec {
	ref := hpa.Spec.ScaleTargetRef
	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	target := snapshot.ScaleTarget(ctx)
	switch target.State {
	case observation.StateUnavailable:
		addCapacityObservationError(input, hpaanalysis.CapacityObservationScaleTarget, "scale target", target.Err)
	case observation.StateNotApplicable:
		addCapacityObservationError(input, hpaanalysis.CapacityObservationScaleTarget, "scale target", fmt.Errorf("resource information is unavailable for %s/%s", ref.Kind, ref.Name))
	case observation.StateKnown:
		info := target.Data
		if info == nil {
			addCapacityObservationError(input, hpaanalysis.CapacityObservationScaleTarget, "scale target", fmt.Errorf("resource information is unavailable for %s/%s", ref.Kind, ref.Name))
			return nil
		}
		collectScaleTargetResources(info, ref.Kind, ref.Name, input)
		var podSpec *corev1.PodSpec
		if info.PodTemplate != nil {
			podSpec = &info.PodTemplate.Spec
		}
		if info.SelectorStr == "" {
			addCapacityObservationError(input, hpaanalysis.CapacityObservationPendingPods, "scale target Pod selector", fmt.Errorf("selector is empty"))
			return podSpec
		}
		pods := snapshot.PodInfos(ctx)
		if pods.State == observation.StateUnavailable {
			addCapacityObservationError(input, hpaanalysis.CapacityObservationPendingPods, "scale target Pods", pods.Err)
			return podSpec
		}
		if pods.Known() {
			collectScaleTargetPodInfos(pods.Data, input)
		}
		return podSpec
	}
	return nil
}

func collectScaleTargetResources(info *kube.ScaleTargetInfo, kind, name string, input *hpaanalysis.CapacityPlanInput) {
	resources := kube.ResourceRequestsFromPodTemplate(info.PodTemplate)
	if resources == nil {
		addCapacityObservationError(input, hpaanalysis.CapacityObservationPodResources, "Pod resource requests", fmt.Errorf("pod template is unavailable for %s/%s", kind, name))
		return
	}
	input.ContainerResources = convertToCapacityContainerResources(resources)
	input.PodRequestCPU = resources.PodRequests["cpu"]
	input.PodRequestMemory = resources.PodRequests["memory"]
	input.PodLimitCPU = resources.PodLimits["cpu"]
	input.PodLimitMemory = resources.PodLimits["memory"]
	input.PodLevelRequestCPU = resources.PodLevelRequests["cpu"]
	input.PodLevelRequestMemory = resources.PodLevelRequests["memory"]
	input.PodLevelLimitCPU = resources.PodLevelLimits["cpu"]
	input.PodLevelLimitMemory = resources.PodLevelLimits["memory"]
}

func collectScaleTargetPodInfos(podInfos []kube.PodInfo, input *hpaanalysis.CapacityPlanInput) {
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
		addCapacityObservationError(input, hpaanalysis.CapacityObservationResourceQuotas, "ResourceQuotas", quotaErr)
	} else {
		input.Quotas = convertToCapacityQuotas(quotaInfos)
	}
}

func collectCapacityLimitRanges(ctx context.Context, client *kube.Client, namespace string, input *hpaanalysis.CapacityPlanInput) {
	lrInfos, limitRangeErr := kube.FetchLimitRanges(ctx, client.Interface, namespace)
	if limitRangeErr != nil {
		addCapacityObservationError(input, hpaanalysis.CapacityObservationLimitRanges, "LimitRanges", limitRangeErr)
	} else {
		input.LimitRanges = convertToCapacityLimitRanges(lrInfos)
	}
}

func collectCapacityClusterHeadroom(ctx context.Context, client *kube.Client, podSpec *corev1.PodSpec, input *hpaanalysis.CapacityPlanInput) {
	if constraints := kube.UnmodeledPodSchedulingConstraints(podSpec); len(constraints) > 0 {
		addCapacityObservationError(
			input,
			hpaanalysis.CapacityObservationNodeCapacity,
			"target Pod scheduling",
			fmt.Errorf("current capacity model cannot evaluate %s", strings.Join(constraints, ", ")),
		)
	}
	clusterHeadroom, headroomErr := kube.FetchClusterResourceHeadroomForPod(ctx, client.Interface, podSpec)
	if headroomErr != nil {
		addCapacityObservationError(input, hpaanalysis.CapacityObservationNodeCapacity, "cluster request headroom", headroomErr)
	} else if clusterHeadroom != nil && clusterHeadroom.NodeCapacity != nil {
		nodeCap := clusterHeadroom.NodeCapacity
		input.NodeCapacity = &blocker.NodeCapacitySummary{
			TotalNodes:            nodeCap.TotalNodes,
			SchedulableNodes:      nodeCap.SchedulableNodes,
			SchedulableNodesKnown: true,
			AllocCPU:              nodeCap.AllocCPU.String(),
			AllocMemory:           nodeCap.AllocMemory.String(),
			RequestedCPU:          clusterHeadroom.RequestedCPU.String(),
			RequestedMemory:       clusterHeadroom.RequestedMemory.String(),
			AvailableCPU:          clusterHeadroom.AvailableCPU.String(),
			AvailableMemory:       clusterHeadroom.AvailableMemory.String(),
			PodCapacityKnown:      clusterHeadroom.PodCapacityKnown,
			AllocPods:             nodeCap.AllocPods,
			RequestedPods:         clusterHeadroom.RequestedPods,
			AvailablePods:         clusterHeadroom.AvailablePods,
			TaintedNodes:          nodeCap.TaintedNodes,
		}
		for _, node := range clusterHeadroom.NodeHeadrooms {
			input.NodeCapacity.NodeHeadrooms = append(input.NodeCapacity.NodeHeadrooms, blocker.NodeResourceHeadroom{
				Name:             node.Name,
				AvailableCPU:     node.AvailableCPU.String(),
				AvailableMemory:  node.AvailableMemory.String(),
				PodCapacityKnown: node.PodCapacityKnown,
				AvailablePods:    node.AvailablePods,
			})
		}
	}
}

func collectCapacityPDBs(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, input *hpaanalysis.CapacityPlanInput) {
	pdbInfos, pdbErr := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)
	if pdbErr != nil {
		addCapacityObservationError(input, hpaanalysis.CapacityObservationPDBs, "PodDisruptionBudgets", pdbErr)
	} else {
		input.PDBs = kubeconv.PDBsPlain(pdbInfos)
	}
}

func collectCapacityAutoscaler(ctx context.Context, client *kube.Client, input *hpaanalysis.CapacityPlanInput) {
	clusterAutoscaler, autoscalerErr := kube.DetectClusterAutoscalerWithError(ctx, client.Interface)
	if autoscalerErr != nil {
		addCapacityObservationError(input, hpaanalysis.CapacityObservationClusterAutoscaler, "Cluster Autoscaler detection", autoscalerErr)
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
			Name:        c.Name,
			CPU:         c.Requests["cpu"],
			Memory:      c.Requests["memory"],
			LimitCPU:    c.Limits["cpu"],
			LimitMemory: c.Limits["memory"],
		})
	}
	return result
}

func convertToCapacityQuotas(infos []kube.QuotaInfo) []hpaanalysis.CapacityQuotaInfo {
	return kubeconv.QuotaDetail(infos, func(q kube.QuotaInfo) hpaanalysis.CapacityQuotaInfo {
		usageKnown := q.UsageKnown
		hardKnown := q.HardKnown
		return hpaanalysis.CapacityQuotaInfo{
			Name:          q.Name,
			Resource:      q.Resource,
			Used:          q.Used,
			Hard:          q.Hard,
			UsageObserved: &usageKnown,
			HardObserved:  &hardKnown,
			Scoped:        q.Scoped,
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
			Name:                 lr.Name,
			Type:                 lr.Type,
			Resource:             lr.Resource,
			Min:                  lr.Min,
			Max:                  lr.Max,
			Default:              lr.Default,
			DefaultRequest:       lr.DefaultRequest,
			MaxLimitRequestRatio: lr.MaxLimitRequestRatio,
		})
	}
	return result
}
