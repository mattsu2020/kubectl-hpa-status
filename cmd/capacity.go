package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kubeconv"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildCapacityContext gathers infrastructure capacity information relevant to
// the HPA scale target: pending pods, ResourceQuota limits, PDB interference,
// and node capacity hints.
func buildCapacityContext(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.CapacityContext {
	return buildCapacityContextWithSnapshot(ctx, client, hpa, observation.New(client.Interface, hpa))
}

func buildCapacityContextWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, snapshot *observation.Snapshot) *hpaanalysis.CapacityContext {
	result := &hpaanalysis.CapacityContext{}

	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	target := snapshot.ScaleTarget(ctx)
	if target.State == observation.StateUnavailable {
		result.Warnings = append(result.Warnings, fmt.Sprintf("scale target selector unavailable: %v", target.Err))
		return result
	}
	if !target.Known() || target.Data.SelectorStr == "" {
		return result
	}

	pending := snapshot.PendingPods(ctx)
	switch pending.State {
	case observation.StateKnown:
		result.PendingPods = kubeconv.PendingPodInfos(pending.Data)
	case observation.StateUnavailable:
		result.Warnings = append(result.Warnings, fmt.Sprintf("pending pods unavailable: %v", pending.Err))
	}

	quotaInfos, quotaErr := kube.FetchResourceQuotas(ctx, client.Interface, hpa.Namespace)
	if quotaErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("resource quotas unavailable: %v", quotaErr))
	} else {
		result.QuotaConstraints = kubeconv.Quotas(quotaInfos)
	}

	pdbInfos, pdbErr := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace)
	if pdbErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pod disruption budgets unavailable: %v", pdbErr))
	} else {
		result.PDBInterference = kubeconv.PDBs(pdbInfos)
	}

	result.NodeHints = formatCapacityNodeHints(kube.DetectNodeHintObservations(pending.Data, quotaInfos))

	return result
}

func formatCapacityNodeHints(observations []kube.NodeHintObservation) []string {
	var hints []string
	for _, observation := range observations {
		switch observation.Kind {
		case kube.NodeHintUnschedulable:
			hints = append(hints, fmt.Sprintf("%d pending pod(s) are unschedulable; consider enabling Cluster Autoscaler or Karpenter for node auto-scaling", observation.Count))
		case kube.NodeHintQuota:
			hints = append(hints, fmt.Sprintf("ResourceQuota %q is near limit for %s; HPA scale-up may hit quota", observation.Name, observation.Resource))
		}
	}
	return hints
}

// capacitySelector resolves the label selector for the HPA scale target.
func capacitySelector(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	selector, _ := capacitySelectorWithError(ctx, client, hpa)
	return selector
}

func capacitySelectorWithError(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) (string, error) {
	selector, err := scaleTargetSelector(ctx, client, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || selector == nil {
		return "", err
	}

	return metav1.FormatLabelSelector(selector), nil
}

// scaleTargetSelector resolves the label selector of the HPA's scale target.
// Returns (nil, nil) when the scale target kind is not one we recognise;
// callers must check for a nil selector before using it.
//
//nolint:nilnil // nil selector with no error is intentional for unsupported kinds
func scaleTargetSelector(
	ctx context.Context,
	client *kube.Client,
	namespace string,
	ref autoscalingv2.CrossVersionObjectReference,
) (*metav1.LabelSelector, error) {
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, namespace, ref)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return info.Selector, nil
}
