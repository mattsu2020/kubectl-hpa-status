package cmd

import (
	"context"
	"fmt"

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

	selector, err := capacitySelectorWithError(ctx, client, hpa)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("scale target selector unavailable: %v", err))
		return result
	}
	if selector == "" {
		return result
	}

	pendingDetails, pendingErr := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
	if pendingErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pending pods unavailable: %v", pendingErr))
	} else {
		result.PendingPods = convertPendingPodInfos(pendingDetails)
	}

	quotaInfos, quotaErr := kube.FetchResourceQuotas(ctx, client.Interface, hpa.Namespace)
	if quotaErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("resource quotas unavailable: %v", quotaErr))
	} else {
		result.QuotaConstraints = convertQuotas(quotaInfos)
	}

	pdbInfos, pdbErr := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)
	if pdbErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pod disruption budgets unavailable: %v", pdbErr))
	} else {
		result.PDBInterference = convertPDBs(pdbInfos)
	}

	result.NodeHints = kube.GenerateNodeHints(pendingDetails, quotaInfos)

	return result
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
