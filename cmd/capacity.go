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

	selector := capacitySelector(ctx, client, hpa)
	if selector == "" {
		return result
	}

	pendingDetails, _ := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
	result.PendingPods = convertPendingPodInfos(pendingDetails)

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

func convertQuotas(infos []kube.QuotaInfo) []hpaanalysis.QuotaConstraint {
	if len(infos) == 0 {
		return nil
	}
	result := make([]hpaanalysis.QuotaConstraint, 0, len(infos))
	for _, q := range infos {
		result = append(result, hpaanalysis.QuotaConstraint{
			Name:     q.Name,
			Resource: q.Resource,
			Used:     q.Used,
			Hard:     q.Hard,
			Message:  fmt.Sprintf("ResourceQuota %q is near limit for %s (used=%s, hard=%s)", q.Name, q.Resource, q.Used, q.Hard),
		})
	}
	return result
}

func convertPDBs(infos []kube.PDBInfo) []hpaanalysis.PDBInterference {
	if len(infos) == 0 {
		return nil
	}
	result := make([]hpaanalysis.PDBInterference, 0, len(infos))
	for _, p := range infos {
		disruption := ""
		if p.MinAvailable != "" {
			disruption = fmt.Sprintf("minAvailable=%s may delay scale-down during disruptions", p.MinAvailable)
		}
		if p.MaxUnavailable != "" {
			disruption = fmt.Sprintf("maxUnavailable=%s may limit concurrent disruptions", p.MaxUnavailable)
		}
		if disruption == "" {
			disruption = "PDB present but no availability constraint specified"
		}
		result = append(result, hpaanalysis.PDBInterference{
			Name:           p.Name,
			MinAvailable:   p.MinAvailable,
			MaxUnavailable: p.MaxUnavailable,
			Disruption:     disruption,
		})
	}
	return result
}
