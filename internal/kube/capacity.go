package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	// HardKnown reports whether status.hard contained this resource. Scoped
	// reports quotas whose Pod applicability is not universal.
	HardKnown bool
	Scoped    bool
	// UsageKnown reports whether status.used contained this hard resource.
	UsageKnown bool
}

// PDBInfo holds information about a PodDisruptionBudget.
type PDBInfo struct {
	Name           string
	MinAvailable   string
	MaxUnavailable string
}

// FetchPendingPodDetails lists pods matching the selector and returns details
// about pending/unschedulable pods. A nil slice with a nil error means no
// pending pods were found; a non-nil error means the pods list call failed.
func FetchPendingPodDetails(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]PendingPodDetail, error) {
	if selector == "" {
		return nil, nil
	}

	pods, err := listPods(ctx, client, namespace, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list pending pods: %w", err)
	}
	return PendingPodDetailsFromPods(pods), nil
}

// PendingPodDetailsFromPods derives pending scheduling details from an
// already-fetched workload snapshot.
func PendingPodDetailsFromPods(pods []corev1.Pod) []PendingPodDetail {
	var pending []PendingPodDetail
	for _, pod := range pods {
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
// quotas where resource usage is at or above 80% of the hard limit. A nil slice
// with a nil error means no near-limit quotas were found.
func FetchResourceQuotas(ctx context.Context, client kubernetes.Interface, namespace string) ([]QuotaInfo, error) {
	quotas, err := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list resource quotas: %w", err)
	}

	var constraints []QuotaInfo
	for _, quota := range quotas.Items {
		for resourceName, hard := range quota.Status.Hard {
			used, usageKnown := quota.Status.Used[resourceName]
			if !usageKnown {
				continue
			}
			if used.IsZero() && hard.IsZero() {
				continue
			}
			if !hard.IsZero() {
				ratio := used.AsApproximateFloat64() / hard.AsApproximateFloat64()
				if ratio >= 0.8 {
					constraints = append(constraints, QuotaInfo{
						Name:       quota.Name,
						Resource:   string(resourceName),
						Used:       used.String(),
						Hard:       hard.String(),
						Ratio:      ratio,
						HardKnown:  true,
						Scoped:     quotaIsScoped(quota),
						UsageKnown: true,
					})
				}
			}
		}
	}
	return constraints, nil
}

// FetchPodDisruptionBudgets lists all PDBs in the namespace. Note: this returns
// all PDBs regardless of whether they match the HPA scale target, since PDB
// selector matching requires resolving pod labels which may not be available
// in the current context. Consumers should filter as needed.
func FetchPodDisruptionBudgets(ctx context.Context, client kubernetes.Interface, namespace string, _ types.UID) ([]PDBInfo, error) {
	pdbs, err := client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pod disruption budgets: %w", err)
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
	return matches, nil
}

// LimitRangeInfo holds parsed LimitRange constraints relevant to pod scheduling.
type LimitRangeInfo struct {
	Name                 string
	Type                 string // "Container" or "Pod"
	Resource             string // "cpu", "memory", etc.
	Min                  string // empty if no minimum
	Max                  string // empty if no maximum
	Default              string // default container limit
	DefaultRequest       string // default container request
	MaxLimitRequestRatio string // empty if no ratio constraint
}

// FetchLimitRanges lists LimitRange objects in the namespace and returns
// all resource constraints for Container and Pod types.
func FetchLimitRanges(ctx context.Context, client kubernetes.Interface, namespace string) ([]LimitRangeInfo, error) {
	ranges, err := client.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list limit ranges: %w", err)
	}

	var constraints []LimitRangeInfo
	for _, lr := range ranges.Items {
		for _, item := range lr.Spec.Limits {
			lrType := string(item.Type)
			if lrType != "Container" && lrType != "Pod" {
				continue
			}
			resourceNames := make(map[corev1.ResourceName]struct{})
			for resourceName := range item.Min {
				resourceNames[resourceName] = struct{}{}
			}
			for resourceName := range item.Max {
				resourceNames[resourceName] = struct{}{}
			}
			for resourceName := range item.Default {
				resourceNames[resourceName] = struct{}{}
			}
			for resourceName := range item.DefaultRequest {
				resourceNames[resourceName] = struct{}{}
			}
			for resourceName := range item.MaxLimitRequestRatio {
				resourceNames[resourceName] = struct{}{}
			}
			sortedResourceNames := make([]string, 0, len(resourceNames))
			for resourceName := range resourceNames {
				sortedResourceNames = append(sortedResourceNames, string(resourceName))
			}
			sort.Strings(sortedResourceNames)
			for _, resourceNameString := range sortedResourceNames {
				resourceName := corev1.ResourceName(resourceNameString)
				constraints = append(constraints, LimitRangeInfo{
					Name:                 lr.Name,
					Type:                 lrType,
					Resource:             resourceNameString,
					Min:                  quantityString(item.Min, resourceName),
					Max:                  quantityString(item.Max, resourceName),
					Default:              quantityString(item.Default, resourceName),
					DefaultRequest:       quantityString(item.DefaultRequest, resourceName),
					MaxLimitRequestRatio: quantityString(item.MaxLimitRequestRatio, resourceName),
				})
			}
		}
	}
	return constraints, nil
}

func quantityString(resources corev1.ResourceList, name corev1.ResourceName) string {
	quantity, ok := resources[name]
	if !ok {
		return ""
	}
	return quantity.String()
}

// FetchAllResourceQuotas lists all ResourceQuotas in the namespace regardless
// of usage ratio. Unlike FetchResourceQuotas (which filters to >= 80%), this
// returns all quotas so the caller can compute remaining headroom.
func FetchAllResourceQuotas(ctx context.Context, client kubernetes.Interface, namespace string) ([]QuotaInfo, error) {
	quotas, err := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list all resource quotas: %w", err)
	}

	var all []QuotaInfo
	for _, quota := range quotas.Items {
		resourceNames := quotaResourceNames(quota)
		for _, resourceName := range resourceNames {
			hard, hardKnown := quota.Status.Hard[resourceName]
			used, usageKnown := quota.Status.Used[resourceName]
			var ratio float64
			if hardKnown && usageKnown && !hard.IsZero() {
				ratio = used.AsApproximateFloat64() / hard.AsApproximateFloat64()
			}
			all = append(all, QuotaInfo{
				Name:       quota.Name,
				Resource:   string(resourceName),
				Used:       used.String(),
				Hard:       hard.String(),
				Ratio:      ratio,
				HardKnown:  hardKnown,
				Scoped:     quotaIsScoped(quota),
				UsageKnown: usageKnown,
			})
		}
	}
	return all, nil
}

func quotaResourceNames(quota corev1.ResourceQuota) []corev1.ResourceName {
	names := make(map[string]struct{}, len(quota.Spec.Hard)+len(quota.Status.Hard))
	for name := range quota.Spec.Hard {
		names[string(name)] = struct{}{}
	}
	for name := range quota.Status.Hard {
		names[string(name)] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	result := make([]corev1.ResourceName, len(sorted))
	for i := range sorted {
		result[i] = corev1.ResourceName(sorted[i])
	}
	return result
}

func quotaIsScoped(quota corev1.ResourceQuota) bool {
	return len(quota.Spec.Scopes) > 0 ||
		(quota.Spec.ScopeSelector != nil && len(quota.Spec.ScopeSelector.MatchExpressions) > 0)
}

// DetectClusterAutoscaler attempts to detect whether Cluster Autoscaler is
// active in the cluster. It uses two heuristics: (1) nodes with the CA-specific
// annotation "cluster-autoscaler.kubernetes.io/safe-to-evict", and (2) a
// Deployment named "cluster-autoscaler" in kube-system. Returns true if either
// signal is found. This is best-effort and may produce false negatives.
func DetectClusterAutoscaler(ctx context.Context, client kubernetes.Interface) bool {
	detected, _ := DetectClusterAutoscalerWithError(ctx, client)
	return detected
}

// DetectClusterAutoscalerWithError performs the same detection while
// preserving read failures. A NotFound deployment is an expected negative
// signal; RBAC, transport, and node-list failures make a negative result
// unknown.
func DetectClusterAutoscalerWithError(ctx context.Context, client kubernetes.Interface) (bool, error) {
	// Check nodes for CA annotation.
	nodes, nodeErr := listNodes(ctx, client, metav1.ListOptions{})
	if nodeErr == nil {
		for _, node := range nodes {
			if _, ok := node.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"]; ok {
				return true, nil
			}
		}
	}

	// Check for CA deployment in kube-system.
	deploy, deployErr := client.AppsV1().Deployments("kube-system").Get(ctx, "cluster-autoscaler", metav1.GetOptions{})
	if deployErr == nil && deploy != nil {
		return true, nil
	}
	if apierrors.IsNotFound(deployErr) {
		deployErr = nil
	}

	var detectionErrors []error
	if nodeErr != nil {
		detectionErrors = append(detectionErrors, fmt.Errorf("list nodes: %w", nodeErr))
	}
	if deployErr != nil {
		detectionErrors = append(detectionErrors, fmt.Errorf("get kube-system/cluster-autoscaler: %w", deployErr))
	}
	if len(detectionErrors) > 0 {
		return false, errors.Join(detectionErrors...)
	}
	return false, nil
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
