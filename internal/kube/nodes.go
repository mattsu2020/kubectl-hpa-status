package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeCapacityInfo holds aggregated node-level capacity information.
type NodeCapacityInfo struct {
	TotalNodes   int32
	AllocCPU     resource.Quantity
	AllocMemory  resource.Quantity
	TaintedNodes int32
}

// FetchNodeCapacity lists all nodes and returns an aggregate capacity summary.
// It sums allocatable CPU and memory and counts tainted nodes (NoSchedule/NoExecute).
func FetchNodeCapacity(ctx context.Context, client kubernetes.Interface) (*NodeCapacityInfo, error) {
	nodes, err := listNodes(ctx, client, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var totalCPU, totalMemory resource.Quantity
	var taintedNodes int32

	for _, node := range nodes {
		if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
			totalCPU.Add(cpu)
		}
		if mem, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
			totalMemory.Add(mem)
		}
		if hasBlockingTaint(node.Spec.Taints) {
			taintedNodes++
		}
	}

	return &NodeCapacityInfo{
		TotalNodes:   int32(len(nodes)),
		AllocCPU:     totalCPU,
		AllocMemory:  totalMemory,
		TaintedNodes: taintedNodes,
	}, nil
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
