package cmd

import (
	"context"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildCapacityContext gathers infrastructure capacity information relevant to
// the HPA scale target: pending pods, ResourceQuota limits, PDB interference,
// and node capacity hints.
func buildCapacityContext(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.CapacityContext {
	result := &hpaanalysis.CapacityContext{}

	selector := capacitySelector(ctx, client, hpa)
	if selector == "" {
		return result
	}

	pendingDetails, _ := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
	result.PendingPods = convertToPendingPodInfos(pendingDetails)

	quotaInfos, _ := kube.FetchResourceQuotas(ctx, client.Interface, hpa.Namespace)
	result.QuotaConstraints = convertQuotas(quotaInfos)

	pdbInfos, _ := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)
	result.PDBInterference = convertPDBs(pdbInfos)

	result.NodeHints = kube.GenerateNodeHints(pendingDetails, quotaInfos)

	return result
}

// capacitySelector resolves the label selector for the HPA scale target.
func capacitySelector(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	selector, err := scaleTargetSelector(ctx, client, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || selector == nil {
		return ""
	}

	return metav1.FormatLabelSelector(selector)
}

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
