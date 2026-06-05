package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
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

	pendingDetails := kube.FetchPendingPodDetails(ctx, client.Interface, hpa.Namespace, selector)
	result.PendingPods = convertPendingPods(pendingDetails)

	quotaInfos := kube.FetchResourceQuotas(ctx, client.Interface, hpa.Namespace)
	result.QuotaConstraints = convertQuotas(quotaInfos)

	pdbInfos := kube.FetchPodDisruptionBudgets(ctx, client.Interface, hpa.Namespace, hpa.UID)
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
	apps := client.Interface.AppsV1()

	switch ref.Kind {
	case "Deployment":
		return deploymentSelector(ctx, apps, namespace, ref.Name)

	case "StatefulSet":
		return statefulSetSelector(ctx, apps, namespace, ref.Name)

	case "ReplicaSet":
		return replicaSetSelector(ctx, apps, namespace, ref.Name)

	default:
		return nil, nil
	}
}

func deploymentSelector(
	ctx context.Context,
	apps appsv1client.AppsV1Interface,
	namespace string,
	name string,
) (*metav1.LabelSelector, error) {
	deploy, err := apps.Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Deployment %s/%s: %w", namespace, name, err)
	}
	return deploy.Spec.Selector, nil
}

func statefulSetSelector(
	ctx context.Context,
	apps appsv1client.AppsV1Interface,
	namespace string,
	name string,
) (*metav1.LabelSelector, error) {
	sts, err := apps.StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet %s/%s: %w", namespace, name, err)
	}
	return sts.Spec.Selector, nil
}

func replicaSetSelector(
	ctx context.Context,
	apps appsv1client.AppsV1Interface,
	namespace string,
	name string,
) (*metav1.LabelSelector, error) {
	rs, err := apps.ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ReplicaSet %s/%s: %w", namespace, name, err)
	}
	return rs.Spec.Selector, nil
}

func convertPendingPods(details []kube.PendingPodDetail) []hpaanalysis.PendingPodInfo {
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
