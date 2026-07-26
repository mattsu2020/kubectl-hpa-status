package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterResourceHeadroom is aggregate request headroom across visible nodes.
// It intentionally models scheduler requests rather than live utilization.
type ClusterResourceHeadroom struct {
	NodeCapacity    *NodeCapacityInfo
	RequestedCPU    resource.Quantity
	RequestedMemory resource.Quantity
	AvailableCPU    resource.Quantity
	AvailableMemory resource.Quantity
}

// FetchClusterResourceHeadroom subtracts the effective requests of every
// scheduled, non-terminal Pod in the cluster from aggregate node allocatable
// resources. Cluster-wide Pod list permission is required; callers must treat
// an error as unknown capacity rather than as empty usage.
func FetchClusterResourceHeadroom(ctx context.Context, client kubernetes.Interface) (*ClusterResourceHeadroom, error) {
	nodeCapacity, err := FetchNodeCapacity(ctx, client)
	if err != nil {
		return nil, err
	}
	requestedCPU, requestedMemory, err := FetchScheduledPodRequests(ctx, client)
	if err != nil {
		return nil, err
	}

	availableCPU := nodeCapacity.AllocCPU.DeepCopy()
	availableCPU.Sub(requestedCPU)
	availableMemory := nodeCapacity.AllocMemory.DeepCopy()
	availableMemory.Sub(requestedMemory)

	return &ClusterResourceHeadroom{
		NodeCapacity:    nodeCapacity,
		RequestedCPU:    requestedCPU,
		RequestedMemory: requestedMemory,
		AvailableCPU:    availableCPU,
		AvailableMemory: availableMemory,
	}, nil
}

// FetchScheduledPodRequests returns effective requests for all scheduled,
// non-terminal Pods across all namespaces.
func FetchScheduledPodRequests(ctx context.Context, client kubernetes.Interface) (resource.Quantity, resource.Quantity, error) {
	pods, err := listPods(ctx, client, metav1.NamespaceAll, metav1.ListOptions{})
	if err != nil {
		return resource.Quantity{}, resource.Quantity{}, fmt.Errorf("failed to list scheduled pods: %w", err)
	}

	var cpu, memory resource.Quantity
	for i := range pods {
		pod := &pods[i]
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		requests := EffectivePodRequests(pod.Spec)
		if quantity, ok := requests[corev1.ResourceCPU]; ok {
			cpu.Add(quantity)
		}
		if quantity, ok := requests[corev1.ResourceMemory]; ok {
			memory.Add(quantity)
		}
	}
	return cpu, memory, nil
}
