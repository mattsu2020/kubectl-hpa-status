package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// NodeCapacityInfo holds aggregated node-level capacity information.
type NodeCapacityInfo struct {
	TotalNodes         int32
	SchedulableNodes   int32
	AllocCPU           resource.Quantity
	AllocMemory        resource.Quantity
	AllocPods          int64
	PodCapacityKnown   bool
	TaintedNodes       int32
	NotReadyNodes      int32
	UnschedulableNodes int32
}

// FetchNodeCapacity lists all nodes and returns an aggregate capacity summary.
// Allocatable CPU and memory include only Ready, uncordoned nodes without a
// blocking NoSchedule/NoExecute taint; excluded-node counts remain visible.
func FetchNodeCapacity(ctx context.Context, client kubernetes.Interface) (*NodeCapacityInfo, error) {
	nodes, err := listNodes(ctx, client, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return summarizeNodeCapacity(nodes), nil
}

func summarizeNodeCapacity(nodes []corev1.Node) *NodeCapacityInfo {
	return summarizeNodeCapacityForPod(nodes, nil)
}

func summarizeNodeCapacityForPod(nodes []corev1.Node, podSpec *corev1.PodSpec) *NodeCapacityInfo {
	info := &NodeCapacityInfo{TotalNodes: int32(len(nodes))}
	for i := range nodes {
		node := &nodes[i]
		ready := nodeReady(node)
		tainted := hasBlockingTaint(node.Spec.Taints)
		if !ready {
			info.NotReadyNodes++
		}
		if node.Spec.Unschedulable {
			info.UnschedulableNodes++
		}
		if tainted {
			info.TaintedNodes++
		}
		if !nodeEligibleForPod(node, podSpec) {
			continue
		}
		info.SchedulableNodes++
		if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			info.AllocCPU.Add(cpu)
		}
		if memory, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			info.AllocMemory.Add(memory)
		}
		if pods, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
			info.AllocPods += pods.Value()
		} else {
			info.PodCapacityKnown = false
		}
	}
	if info.SchedulableNodes > 0 {
		info.PodCapacityKnown = true
		for i := range nodes {
			if !nodeEligibleForPod(&nodes[i], podSpec) {
				continue
			}
			if _, ok := nodes[i].Status.Allocatable[corev1.ResourcePods]; !ok {
				info.PodCapacityKnown = false
				break
			}
		}
	}
	return info
}

func nodeReady(node *corev1.Node) bool {
	if node == nil {
		return false
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func nodeEligibleForUntoleratedPod(node *corev1.Node) bool { //nolint:unused // Compatibility helper retained for package-local callers.
	return nodeEligibleForPod(node, nil)
}

// nodeEligibleForPod evaluates the scheduling constraints that can be decided
// from a Node and PodSpec alone. Constraints that need other cluster objects
// (affinity peers, topology domains, RuntimeClass, volumes, custom schedulers)
// are reported separately by UnmodeledPodSchedulingConstraints.
func nodeEligibleForPod(node *corev1.Node, podSpec *corev1.PodSpec) bool { //nolint:gocyclo // Mirrors independent Kubernetes scheduling predicates.
	if !nodeReady(node) || node.Spec.Unschedulable {
		return false
	}
	if podSpec != nil {
		if podSpec.NodeName != "" && podSpec.NodeName != node.Name {
			return false
		}
		for key, value := range podSpec.NodeSelector {
			nodeValue, exists := node.Labels[key]
			if !exists || nodeValue != value {
				return false
			}
		}
	}
	for i := range node.Spec.Taints {
		taint := &node.Spec.Taints[i]
		if taint.Effect != corev1.TaintEffectNoSchedule && taint.Effect != corev1.TaintEffectNoExecute {
			continue
		}
		tolerated := false
		if podSpec != nil {
			for j := range podSpec.Tolerations {
				if podSpec.Tolerations[j].ToleratesTaint(klog.Background(), taint, true) {
					tolerated = true
					break
				}
			}
		}
		if !tolerated {
			return false
		}
	}
	return true
}

func listNodes(ctx context.Context, client kubernetes.Interface, opts metav1.ListOptions) ([]corev1.Node, error) {
	return collectListPages(ctx, opts, func(ctx context.Context, page metav1.ListOptions) ([]corev1.Node, string, error) {
		list, err := client.CoreV1().Nodes().List(ctx, page)
		if err != nil {
			return nil, "", err
		}
		return list.Items, list.Continue, nil
	})
}

// hasBlockingTaint returns true if any taint has NoSchedule or NoExecute effect,
// indicating the node may reject pods without matching tolerations.
func hasBlockingTaint(taints []corev1.Taint) bool {
	for _, taint := range taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return true
		}
	}
	return false
}
