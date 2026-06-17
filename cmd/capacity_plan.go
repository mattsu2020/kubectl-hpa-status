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
			if local.Output == "json" || local.Output == "yaml" {
				writeError(out, local.Output, err)
			}
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
			if err := hpaanalysis.WriteCapacityPlanText(out, o.Plan, theme); err != nil {
				return err
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
		return nil
	}

	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return nil
	}

	input := assembleCapacityPlanInput(ctx, client, hpa, analysis, opts.TargetMax)
	return hpaanalysis.AnalyzeCapacityPlan(input)
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

	// Resolve scale target info and pod template resources.
	ref := hpa.Spec.ScaleTargetRef
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, ref)
	if err == nil && info != nil {
		selector := info.SelectorStr
		if selector != "" {
			// Fetch pod info for ready count.
			podInfos, _ := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, selector)
			var ready int32
			for _, p := range podInfos {
				if p.Ready {
					ready++
				}
			}
			input.ReadyPods = ready

			// Fetch pending pod details.
			pendingDetails, _ := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
			input.PendingPods = convertPendingPodInfos(pendingDetails)
		}
	}

	// Fetch container resources from pod template.
	resources, err := kube.FetchScaleTargetResources(ctx, client.Interface, hpa.Namespace, ref.Kind, ref.Name)
	if err == nil && resources != nil {
		input.ContainerResources = convertToCapacityContainerResources(resources)
	}

	// Fetch all ResourceQuotas (not just near-limit).
	quotaInfos, _ := kube.FetchAllResourceQuotas(ctx, client.Interface, hpa.Namespace)
	input.Quotas = convertToCapacityQuotas(quotaInfos)

	// Fetch LimitRanges.
	lrInfos, _ := kube.FetchLimitRanges(ctx, client.Interface, hpa.Namespace)
	input.LimitRanges = convertToCapacityLimitRanges(lrInfos)

	// Fetch node capacity.
	nodeCap, _ := kube.FetchNodeCapacity(ctx, client.Interface)
	if nodeCap != nil {
		input.NodeCapacity = &hpaanalysis.NodeCapacitySummary{
			TotalNodes:   nodeCap.TotalNodes,
			AllocCPU:     nodeCap.AllocCPU.String(),
			AllocMemory:  nodeCap.AllocMemory.String(),
			TaintedNodes: nodeCap.TaintedNodes,
		}
	}

	// Fetch PDBs.
	pdbInfos, _ := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)
	input.PDBs = convertPDBsPlain(pdbInfos)

	// Detect Cluster Autoscaler.
	input.ClusterAutoscaler = kube.DetectClusterAutoscaler(ctx, client.Interface)

	return input
}

// ---------------------------------------------------------------------------
// Converter functions
// ---------------------------------------------------------------------------

func convertToCapacityContainerResources(rr *kube.ResourceRequests) []hpaanalysis.CapacityContainerResources {
	if rr == nil {
		return nil
	}
	result := make([]hpaanalysis.CapacityContainerResources, 0, len(rr.Containers))
	for _, c := range rr.Containers {
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
