package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// PendingPodDetail holds information about a pending pod.
type PendingPodDetail struct {
	Name          string
	Unschedulable bool
	Reasons       []string
}

// QuotaInfo holds information about a ResourceQuota.
type QuotaInfo struct {
	Name     string
	Resource string
	Used     string
	Hard     string
	Ratio    float64
}

// PDBInfo holds information about a PodDisruptionBudget.
type PDBInfo struct {
	Name           string
	MinAvailable   string
	MaxUnavailable string
}

// FetchPendingPodDetails lists pods matching the selector and returns details
// about pending/unschedulable pods.
func FetchPendingPodDetails(ctx context.Context, client kubernetes.Interface, namespace, selector string) []PendingPodDetail {
	if selector == "" {
		return nil
	}

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil
	}

	var pending []PendingPodDetail
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodPending {
			continue
		}
		detail := PendingPodDetail{
			Name: pod.Name,
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodScheduled &&
				condition.Status == corev1.ConditionFalse &&
				condition.Reason == corev1.PodReasonUnschedulable {
				detail.Unschedulable = true
				detail.Reasons = append(detail.Reasons, condition.Message)
			}
		}
		pending = append(pending, detail)
	}
	return pending
}

// FetchResourceQuotas lists ResourceQuotas in the namespace and returns
// quotas where resource usage is at or above 80% of the hard limit.
func FetchResourceQuotas(ctx context.Context, client kubernetes.Interface, namespace string) []QuotaInfo {
	quotas, err := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var constraints []QuotaInfo
	for _, quota := range quotas.Items {
		for resourceName, hard := range quota.Spec.Hard {
			used := quota.Status.Used[resourceName]
			if used.IsZero() && hard.IsZero() {
				continue
			}
			if !hard.IsZero() {
				ratio := used.AsApproximateFloat64() / hard.AsApproximateFloat64()
				if ratio >= 0.8 {
					constraints = append(constraints, QuotaInfo{
						Name:     quota.Name,
						Resource: string(resourceName),
						Used:     used.String(),
						Hard:     hard.String(),
						Ratio:    ratio,
					})
				}
			}
		}
	}
	return constraints
}

// FetchPodDisruptionBudgets lists all PDBs in the namespace. Note: this returns
// all PDBs regardless of whether they match the HPA scale target, since PDB
// selector matching requires resolving pod labels which may not be available
// in the current context. Consumers should filter as needed.
func FetchPodDisruptionBudgets(ctx context.Context, client kubernetes.Interface, namespace string, _ types.UID) []PDBInfo {
	pdbs, err := client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var matches []PDBInfo
	for _, pdb := range pdbs.Items {
		info := PDBInfo{
			Name: pdb.Name,
		}
		if pdb.Spec.MinAvailable != nil {
			info.MinAvailable = pdb.Spec.MinAvailable.String()
		}
		if pdb.Spec.MaxUnavailable != nil {
			info.MaxUnavailable = pdb.Spec.MaxUnavailable.String()
		}
		matches = append(matches, info)
	}
	return matches
}

// GenerateNodeHints produces capacity hints based on pending pods and quota state.
func GenerateNodeHints(pending []PendingPodDetail, quotas []QuotaInfo) []string {
	var hints []string

	unschedulable := 0
	for _, p := range pending {
		if p.Unschedulable {
			unschedulable++
		}
	}

	if unschedulable > 0 {
		hints = append(hints, fmt.Sprintf(
			"%d pending pod(s) are unschedulable; consider enabling Cluster Autoscaler or Karpenter for node auto-scaling",
			unschedulable))
	}

	for _, q := range quotas {
		hints = append(hints, fmt.Sprintf(
			"ResourceQuota %q is near limit for %s; HPA scale-up may hit quota",
			q.Name, q.Resource))
	}

	return hints
}
