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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type autoscalerMapOutput struct {
	Namespace string                     `json:"namespace" yaml:"namespace"`
	Name      string                     `json:"name" yaml:"name"`
	Target    string                     `json:"target" yaml:"target"`
	Map       *hpaanalysis.AutoscalerMap `json:"autoscalerMap" yaml:"autoscalerMap"`
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
	outputs := make([]autoscalerMapOutput, 0, len(names))
	for _, name := range names {
		client, err := opts.newClient()
		if err != nil {
			if opts.output == "json" || opts.output == "yaml" {
				writeError(out, opts.output, err)
			}
			return err
		}

		hpa, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if opts.output == "json" || opts.output == "yaml" {
				writeError(out, opts.output, err)
			}
			return err
		}

		input := assembleAutoscalerMapInput(ctx, client, opts, hpa)
		am := hpaanalysis.AnalyzeAutoscalerMap(input)

		outputs = append(outputs, autoscalerMapOutput{
			Namespace: hpa.Namespace,
			Name:      hpa.Name,
			Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
			Map:       am,
		})
	}

	value := any(outputs)
	if len(outputs) == 1 {
		value = outputs[0]
	}

	format, templateStr := outputSelection(outputConfig{
		output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
	})

	return writeOutput(out, format, templateStr, value, func() error {
		theme := style.NewTheme(shouldColorize(opts.color, out))
		for i, o := range outputs {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteAutoscalerMapText(out, o.Map, theme); err != nil {
				return err
			}
		}
		return nil
	})
}

// assembleAutoscalerMapInput gathers all observable signals for autoscaler map.
func assembleAutoscalerMapInput(ctx context.Context, client *kube.Client, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) hpaanalysis.AutoscalerMapInput {
	input := hpaanalysis.AutoscalerMapInput{
		Namespace:       hpa.Namespace,
		HPAName:         hpa.Name,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		DesiredReplicas: hpa.Status.DesiredReplicas,
		MaxReplicas:     hpa.Spec.MaxReplicas,
		ScalingActive:   hpaanalysis.IsScalingActive(hpa),
	}

	ref := hpa.Spec.ScaleTargetRef
	input.Target = fmt.Sprintf("%s/%s", ref.Kind, ref.Name)

	// Fetch scale target info.
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, ref)
	if err == nil && info != nil {
		input.WorkloadReadyReplicas = info.ReadyReplicas
		input.WorkloadDesiredReplicas = info.DesiredReplicas

		selector := info.SelectorStr
		if selector != "" {
			// Fetch pod info.
			podInfos, _ := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, selector)
			var running, pending, ready int32
			for _, p := range podInfos {
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
			input.PodSummary = hpaanalysis.AutoscalerMapPodSummary{
				Total:   int32(len(podInfos)),
				Running: running,
				Pending: pending,
				Ready:   ready,
			}

			// Fetch pending pod details.
			pendingDetails, _ := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
			input.PendingPods = convertToAutoscalerMapPendingPods(pendingDetails)
		}
	}

	// Fetch node capacity.
	nodeCap, _ := kube.FetchNodeCapacity(ctx, client.Interface)
	if nodeCap != nil {
		input.NodeSummary = hpaanalysis.AutoscalerMapNodeSummary{
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
	input.PDBs = fetchAutoscalerMapPDBs(ctx, client, hpa.Namespace)

	// Fetch ResourceQuotas near limits.
	input.Quotas = fetchAutoscalerMapQuotas(ctx, client, hpa.Namespace, hpa.Spec.MaxReplicas)

	return input
}

// convertToAutoscalerMapPendingPods converts internal types to analysis types.
func convertToAutoscalerMapPendingPods(details []kube.PendingPodDetail) []hpaanalysis.PendingPodInfo {
	if len(details) == 0 {
		return nil
	}
	result := make([]hpaanalysis.PendingPodInfo, 0, len(details))
	for _, d := range details {
		result = append(result, hpaanalysis.PendingPodInfo{
			Name:          d.Name,
			Phase:         "Pending",
			Unschedulable: d.Unschedulable,
			Reasons:       d.Reasons,
		})
	}
	return result
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
func fetchAutoscalerMapKEDA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.AutoscalerMapKEDAInfo {
	detection := kube.DetectKEDA(hpa)
	if !detection.Managed {
		return nil
	}

	dynClient, _, err := kube.NewDynamicClient(kube.Options{
		Kubeconfig: opts.kubeconfig,
		Context:    opts.contextName,
		QPS:        opts.qps,
		Burst:      opts.burst,
	})
	if err != nil {
		return nil
	}

	scaledObj, err := kube.FindScaledObjectForHPA(ctx, dynClient, nil, hpa)
	if err != nil || scaledObj == nil {
		return &hpaanalysis.AutoscalerMapKEDAInfo{
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

	return &hpaanalysis.AutoscalerMapKEDAInfo{
		ScaledObjectName: kedaInfo.ScaledObjectName,
		TriggerCount:     len(kedaInfo.Triggers),
		Active:           active,
	}
}

// fetchAutoscalerMapVPA attempts to detect VPA conflicts with the HPA.
func fetchAutoscalerMapVPA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.AutoscalerMapVPAInfo {
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

	dynClient, _, err := kube.NewDynamicClient(kube.Options{
		Kubeconfig: opts.kubeconfig,
		Context:    opts.contextName,
		QPS:        opts.qps,
		Burst:      opts.burst,
	})
	if err != nil {
		return nil
	}

	vpaInfo, err := kube.FindConflictingVPA(ctx, dynClient, hpa.Namespace, hpa)
	if err != nil || vpaInfo == nil {
		return nil
	}

	return &hpaanalysis.AutoscalerMapVPAInfo{
		VPAName:             vpaInfo.Name,
		TargetRef:           vpaInfo.TargetRef,
		UpdateMode:          vpaInfo.UpdateMode,
		ControlledResources: vpaInfo.ControlledResources,
		ConflictResources:   vpaInfo.ControlledResources,
	}
}

// fetchAutoscalerMapPDBs fetches PodDisruptionBudgets in the namespace.
func fetchAutoscalerMapPDBs(ctx context.Context, client *kube.Client, namespace string) []hpaanalysis.AutoscalerMapPDB {
	pdbs, _ := kube.FetchPodDisruptionBudgets(ctx, client.Interface, namespace, "")
	if len(pdbs) == 0 {
		return nil
	}

	result := make([]hpaanalysis.AutoscalerMapPDB, 0, len(pdbs))
	for _, pdb := range pdbs {
		p := hpaanalysis.AutoscalerMapPDB{
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
	return result
}

// fetchAutoscalerMapQuotas fetches ResourceQuotas near their limits (ratio >= 0.7).
func fetchAutoscalerMapQuotas(ctx context.Context, client *kube.Client, namespace string, _ int32) []hpaanalysis.AutoscalerMapQuota {
	quotas, _ := kube.FetchAllResourceQuotas(ctx, client.Interface, namespace)
	if len(quotas) == 0 {
		return nil
	}

	result := make([]hpaanalysis.AutoscalerMapQuota, 0, len(quotas))
	for _, q := range quotas {
		if q.Ratio < 0.7 {
			continue
		}
		result = append(result, hpaanalysis.AutoscalerMapQuota{
			Name:     q.Name,
			Resource: q.Resource,
			Used:     q.Used,
			Hard:     q.Hard,
			Ratio:    q.Ratio,
		})
	}
	return result
}
