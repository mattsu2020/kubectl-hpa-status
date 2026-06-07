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

// LimitRangeInfo holds parsed LimitRange constraints relevant to pod scheduling.
type LimitRangeInfo struct {
	Name     string
	Type     string // "Container" or "Pod"
	Resource string // "cpu", "memory", etc.
	Min      string // empty if no minimum
	Max      string // empty if no maximum
}

// FetchLimitRanges lists LimitRange objects in the namespace and returns
// min/max constraints for Container and Pod types.
func FetchLimitRanges(ctx context.Context, client kubernetes.Interface, namespace string) []LimitRangeInfo {
	ranges, err := client.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var constraints []LimitRangeInfo
	for _, lr := range ranges.Items {
		for _, item := range lr.Spec.Limits {
			lrType := string(item.Type)
			if lrType != "Container" && lrType != "Pod" {
				continue
			}
			for resourceName, minVal := range item.Min {
				maxVal := item.Max[resourceName]
				constraints = append(constraints, LimitRangeInfo{
					Name:     lr.Name,
					Type:     lrType,
					Resource: string(resourceName),
					Min:      minVal.String(),
					Max:      maxVal.String(),
				})
			}
			// Also include max-only constraints (no min defined).
			for resourceName, maxVal := range item.Max {
				if _, hasMin := item.Min[resourceName]; hasMin {
					continue // already added above
				}
				constraints = append(constraints, LimitRangeInfo{
					Name:     lr.Name,
					Type:     lrType,
					Resource: string(resourceName),
					Max:      maxVal.String(),
				})
			}
		}
	}
	return constraints
}

// FetchAllResourceQuotas lists all ResourceQuotas in the namespace regardless
// of usage ratio. Unlike FetchResourceQuotas (which filters to >= 80%), this
// returns all quotas so the caller can compute remaining headroom.
func FetchAllResourceQuotas(ctx context.Context, client kubernetes.Interface, namespace string) []QuotaInfo {
	quotas, err := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var all []QuotaInfo
	for _, quota := range quotas.Items {
		for resourceName, hard := range quota.Spec.Hard {
			used := quota.Status.Used[resourceName]
			var ratio float64
			if !hard.IsZero() {
				ratio = used.AsApproximateFloat64() / hard.AsApproximateFloat64()
			}
			all = append(all, QuotaInfo{
				Name:     quota.Name,
				Resource: string(resourceName),
				Used:     used.String(),
				Hard:     hard.String(),
				Ratio:    ratio,
			})
		}
	}
	return all
}

// DetectClusterAutoscaler attempts to detect whether Cluster Autoscaler is
// active in the cluster. It uses two heuristics: (1) nodes with the CA-specific
// annotation "cluster-autoscaler.kubernetes.io/safe-to-evict", and (2) a
// Deployment named "cluster-autoscaler" in kube-system. Returns true if either
// signal is found. This is best-effort and may produce false negatives.
func DetectClusterAutoscaler(ctx context.Context, client kubernetes.Interface) bool {
	// Check nodes for CA annotation.
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, node := range nodes.Items {
			if _, ok := node.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"]; ok {
				return true
			}
		}
	}

	// Check for CA deployment in kube-system.
	deploy, err := client.AppsV1().Deployments("kube-system").Get(ctx, "cluster-autoscaler", metav1.GetOptions{})
	if err == nil && deploy != nil {
		return true
	}

	return false
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
