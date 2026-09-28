package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/enrichment"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/autoscalermap"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
)

type autoscalerMapOutput struct {
	Namespace string             `json:"namespace" yaml:"namespace"`
	Name      string             `json:"name" yaml:"name"`
	Target    string             `json:"target" yaml:"target"`
	Map       *autoscalermap.Map `json:"autoscalerMap" yaml:"autoscalerMap"`
}

func newAutoscalerMapCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "autoscaler-map NAME [NAME...]",
		Short:             "Visualize the HPA to Node Autoscaler relationship and detect blockers",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutoscalerMap(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runAutoscalerMap(ctx context.Context, out io.Writer, opts *options, names []string) error {
	// Uses the raw error so JSON/YAML output can emit a structured error
	// document via writeError; the standard wrapper would only add an
	// English prefix that breaks the structured output contract.
	//
	// The client is built once for the whole run. It used to be rebuilt per
	// name inside the loop, which re-read and re-parsed the kubeconfig for
	// every HPA; client-go clients are safe to share across the concurrent
	// per-name work below.
	client, err := opts.NewClient()
	if err != nil {
		return writeErrorIfStructured(out, opts.Output, err)
	}

	outputs, err := collectPerHPA(ctx, opts, names, func(ctx context.Context, name string) (autoscalerMapOutput, error) {
		hpa, err := kube.GetHPAFromClient(ctx, client, name)
		if err != nil {
			return autoscalerMapOutput{}, err
		}

		input, warnings := assembleAutoscalerMapInput(ctx, client, opts, hpa)
		report := autoscalermap.Analyze(input)
		report.Warnings = append(report.Warnings, warnings...)

		return autoscalerMapOutput{
			Namespace: hpa.Namespace,
			Name:      hpa.Name,
			Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
			Map:       report,
		}, nil
	})
	if err != nil {
		return writeErrorIfStructured(out, opts.Output, err)
	}

	return renderPerHPA(out, opts, outputs, func(out io.Writer, o autoscalerMapOutput) error {
		theme := themeFor(opts.Color, out)
		if err := autoscalermap.WriteText(out, o.Map, theme); err != nil {
			return fmt.Errorf("write autoscaler-map report for %s/%s: %w", o.Namespace, o.Name, err)
		}
		return nil
	})

}

// assembleAutoscalerMapInput gathers all observable signals for autoscaler map
// from one request-scoped observation snapshot, returning any collection
// warnings alongside the input. Failed reads surface as warnings instead of
// silently degrading to "no pods / no quotas near limit".
func assembleAutoscalerMapInput(ctx context.Context, client *kube.Client, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) (autoscalermap.Input, []string) {
	input := autoscalermap.Input{
		Namespace:       hpa.Namespace,
		HPAName:         hpa.Name,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		DesiredReplicas: hpa.Status.DesiredReplicas,
		MaxReplicas:     hpa.Spec.MaxReplicas,
		ScalingActive:   hpaanalysis.IsScalingActive(hpa),
	}
	var warnings []string

	ref := hpa.Spec.ScaleTargetRef
	input.Target = fmt.Sprintf("%s/%s", ref.Kind, ref.Name)

	// Observe the scale target, pods, and pending details through one
	// memoized snapshot so derived views reuse the same API reads.
	snapshot := observation.New(client.Interface, hpa)
	target := snapshot.ScaleTarget(ctx)
	switch target.State {
	case observation.StateUnavailable:
		warnings = append(warnings, fmt.Sprintf("scale target unavailable: %v", target.Err))
	case observation.StateNotApplicable:
		warnings = append(warnings, fmt.Sprintf(
			"scale target readiness is not observable for %s/%s",
			ref.Kind,
			ref.Name,
		))
	}
	if target.Known() {
		info := target.Data
		input.WorkloadReadyReplicas = info.ReadyReplicas
		input.WorkloadDesiredReplicas = info.DesiredReplicas

		if info.SelectorStr != "" {
			podInfos := snapshot.PodInfos(ctx)
			if podInfos.Known() {
				var running, pending, ready int32
				for _, p := range podInfos.Data {
					switch p.Phase {
					case "Pending":
						pending++
					case "Running":
						running++
					}
					if p.Ready {
						ready++
					}
				}
				input.PodSummary = autoscalermap.PodSummary{
					Total:   int32(len(podInfos.Data)),
					Running: running,
					Pending: pending,
					Ready:   ready,
				}
			} else if podInfos.State == observation.StateUnavailable {
				warnings = append(warnings, fmt.Sprintf("pods unavailable: %v", podInfos.Err))
			}

			pendingDetails := snapshot.PendingPods(ctx)
			if pendingDetails.Known() {
				input.PendingPods = kubeconv.PendingPodInfos(pendingDetails.Data)
			} else if pendingDetails.State == observation.StateUnavailable {
				warnings = append(warnings, fmt.Sprintf("pending pod details unavailable: %v", pendingDetails.Err))
			}
		}
	}

	// Fetch node capacity. Preserve RBAC/network failures as unknown data rather
	// than turning the zero value into a false "no schedulable nodes" blocker.
	nodeCap, nodeErr := kube.FetchNodeCapacity(ctx, client.Interface)
	if nodeErr != nil {
		input.NodeFetchError = nodeErr.Error()
	}
	if nodeCap != nil {
		input.NodeSummary = autoscalermap.NodeSummary{
			TotalNodes:        nodeCap.TotalNodes,
			AllocatableCPU:    nodeCap.AllocCPU.String(),
			AllocatableMemory: nodeCap.AllocMemory.String(),
			TaintedNodes:      nodeCap.TaintedNodes,
		}
	}

	// Detect Cluster Autoscaler.
	input.ClusterAutoscaler = kube.DetectClusterAutoscaler(ctx, client.Interface)

	// Detect Karpenter (check for Karpenter pods in kube-system).
	input.Karpenter = detectKarpenter(ctx, client)

	// Fetch KEDA ScaledObject info if KEDA-managed.
	input.KEDAInfo = fetchAutoscalerMapKEDA(ctx, opts, hpa)

	// Fetch VPA conflict info.
	input.VPAInfo = fetchAutoscalerMapVPA(ctx, opts, hpa)

	// Fetch PodDisruptionBudgets.
	pdbs, pdbErr := fetchAutoscalerMapPDBs(ctx, client, hpa.Namespace)
	if pdbErr != nil {
		warnings = append(warnings, fmt.Sprintf("pod disruption budgets unavailable: %v", pdbErr))
	}
	input.PDBs = pdbs

	// Fetch ResourceQuotas near limits.
	quotas, quotaErr := fetchAutoscalerMapQuotas(ctx, client, hpa.Namespace)
	if quotaErr != nil {
		warnings = append(warnings, fmt.Sprintf("resource quotas unavailable: %v", quotaErr))
	}
	input.Quotas = quotas

	return input, warnings
}

// detectKarpenter checks for Karpenter pods or CRDs.
func detectKarpenter(ctx context.Context, client *kube.Client) bool {
	pods, err := client.Interface.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=karpenter",
	})
	if err != nil {
		return false
	}
	return len(pods.Items) > 0
}

// fetchAutoscalerMapKEDA attempts to detect KEDA and fetch ScaledObject info.
func fetchAutoscalerMapKEDA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) *autoscalermap.KEDAInfo {
	detection := kube.DetectKEDA(hpa)
	if !detection.Managed {
		return nil
	}

	dynClient, _, err := kube.NewDynamicClient(opts.KubeOptions())
	if err != nil {
		// Best-effort: a dynamic-client failure (RBAC denial on keda.sh,
		// missing KEDA CRD) silently skips the KEDA section rather than
		// aborting the whole autoscaler map.
		return nil
	}

	scaledObj, err := kube.FindScaledObjectForHPA(ctx, dynClient, hpa)
	if err != nil || scaledObj == nil {
		return &autoscalermap.KEDAInfo{
			ScaledObjectName: string(detection.Source),
			Active:           false,
		}
	}

	kedaInfo := kube.ExtractKEDAInfo(scaledObj)

	// Determine if active from trigger statuses.
	active := false
	for _, trigger := range kedaInfo.Triggers {
		if trigger.Status == "Active" {
			active = true
			break
		}
	}

	return &autoscalermap.KEDAInfo{
		ScaledObjectName: kedaInfo.ScaledObjectName,
		TriggerCount:     len(kedaInfo.Triggers),
		Active:           active,
	}
}

// fetchAutoscalerMapVPA attempts to detect VPA conflicts with the HPA.
func fetchAutoscalerMapVPA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) *autoscalermap.VPAInfo {
	// Check if HPA uses resource metrics (CPU/memory) that VPA could conflict with.
	hasResourceMetrics := false
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType || m.Type == autoscalingv2.ContainerResourceMetricSourceType {
			hasResourceMetrics = true
			break
		}
	}
	if !hasResourceMetrics {
		return nil
	}

	dynClient, _, err := kube.NewDynamicClient(opts.KubeOptions())
	if err != nil {
		// Best-effort: a dynamic-client failure (RBAC denial on autoscaling.k8s.io,
		// missing VPA CRD) silently skips the VPA section rather than aborting
		// the whole autoscaler map.
		return nil
	}

	vpaInfo, err := enrichment.FindConflictingVPA(ctx, dynClient, hpa.Namespace, hpa)
	if err != nil || vpaInfo == nil {
		return nil
	}

	return &autoscalermap.VPAInfo{
		VPAName:             vpaInfo.Name,
		TargetRef:           vpaInfo.TargetRef,
		UpdateMode:          vpaInfo.UpdateMode,
		ControlledResources: vpaInfo.ControlledResources,
		ConflictResources:   hpavpa.ConflictResources(hpa, kubeconv.VPAInfo(vpaInfo)),
	}
}

// fetchAutoscalerMapPDBs fetches PodDisruptionBudgets in the namespace.
func fetchAutoscalerMapPDBs(ctx context.Context, client *kube.Client, namespace string) ([]autoscalermap.PDB, error) {
	pdbs, err := kube.FetchPodDisruptionBudgets(ctx, client.Interface, namespace)
	if err != nil {
		return nil, err
	}
	if len(pdbs) == 0 {
		return nil, nil
	}

	result := make([]autoscalermap.PDB, 0, len(pdbs))
	for _, pdb := range pdbs {
		p := autoscalermap.PDB{
			Name: pdb.Name,
		}
		if pdb.MinAvailable != "" {
			p.MinAvailable = pdb.MinAvailable
		}
		if pdb.MaxUnavailable != "" {
			p.MaxUnavailable = pdb.MaxUnavailable
		}
		result = append(result, p)
	}
	return result, nil
}

// quotaNearLimitRatio is the usage ratio at which a ResourceQuota is flagged
// in the autoscaler map's constraints layer. It is intentionally lower than
// the blocker rule's 0.95 cutoff: this view surfaces approaching limits as
// context, while blockers reserve its stricter threshold for hard blockers.
const quotaNearLimitRatio = 0.7

// fetchAutoscalerMapQuotas fetches ResourceQuotas near their limits
// (ratio >= quotaNearLimitRatio).
func fetchAutoscalerMapQuotas(ctx context.Context, client *kube.Client, namespace string) ([]autoscalermap.Quota, error) {
	quotas, err := kube.FetchAllResourceQuotas(ctx, client.Interface, namespace)
	if err != nil {
		return nil, err
	}
	if len(quotas) == 0 {
		return nil, nil
	}

	result := make([]autoscalermap.Quota, 0, len(quotas))
	for _, q := range quotas {
		if q.Ratio < quotaNearLimitRatio {
			continue
		}
		result = append(result, autoscalermap.Quota{
			Name:     q.Name,
			Resource: q.Resource,
			Used:     q.Used,
			Hard:     q.Hard,
			Ratio:    q.Ratio,
		})
	}
	return result, nil
}
