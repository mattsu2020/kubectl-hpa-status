package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterResourceHeadroom is request headroom across Ready, uncordoned,
// conservatively untainted nodes. It intentionally models scheduler requests
// rather than live utilization and preserves per-node fragmentation.
type ClusterResourceHeadroom struct {
	NodeCapacity     *NodeCapacityInfo
	RequestedCPU     resource.Quantity
	RequestedMemory  resource.Quantity
	AvailableCPU     resource.Quantity
	AvailableMemory  resource.Quantity
	RequestedPods    int64
	AvailablePods    int64
	PodCapacityKnown bool
	NodeHeadrooms    []NodeResourceHeadroom
}

// NodeResourceHeadroom preserves per-node request headroom so callers do not
// mistake fragmented aggregate capacity for schedulable capacity.
type NodeResourceHeadroom struct {
	Name             string
	AvailableCPU     resource.Quantity
	AvailableMemory  resource.Quantity
	AvailablePods    int64
	PodCapacityKnown bool
}

// FetchClusterResourceHeadroom subtracts the effective requests of every
// scheduled, non-terminal Pod on an eligible node from that node's allocatable
// resources. Cluster-wide Pod list permission is required; callers must treat
// an error as unknown capacity rather than as empty usage.
func FetchClusterResourceHeadroom(ctx context.Context, client kubernetes.Interface) (*ClusterResourceHeadroom, error) {
	return FetchClusterResourceHeadroomForPod(ctx, client, nil)
}

// FetchClusterResourceHeadroomForPod computes request headroom only on nodes
// eligible for the target Pod's directly evaluable scheduling constraints
// (nodeName, nodeSelector, and taint tolerations).
func FetchClusterResourceHeadroomForPod(ctx context.Context, client kubernetes.Interface, podSpec *corev1.PodSpec) (*ClusterResourceHeadroom, error) {
	nodes, err := listNodes(ctx, client, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	nodeCapacity := summarizeNodeCapacityForPod(nodes, podSpec)
	type nodeUsage struct {
		cpu    resource.Quantity
		memory resource.Quantity
		pods   int64
	}
	eligible := make(map[string]*corev1.Node)
	usage := make(map[string]*nodeUsage)
	for i := range nodes {
		node := &nodes[i]
		if !nodeEligibleForPod(node, podSpec) {
			continue
		}
		eligible[node.Name] = node
		usage[node.Name] = &nodeUsage{}
	}

	var requestedCPU, requestedMemory resource.Quantity
	err = visitPods(ctx, client, metav1.NamespaceAll, metav1.ListOptions{}, func(pods []corev1.Pod) error {
		for i := range pods {
			pod := &pods[i]
			nodeUsage := usage[pod.Spec.NodeName]
			if nodeUsage == nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			requests := EffectivePodRequests(pod.Spec)
			if quantity, ok := requests[corev1.ResourceCPU]; ok {
				nodeUsage.cpu.Add(quantity)
				requestedCPU.Add(quantity)
			}
			if quantity, ok := requests[corev1.ResourceMemory]; ok {
				nodeUsage.memory.Add(quantity)
				requestedMemory.Add(quantity)
			}
			nodeUsage.pods++
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list scheduled pods: %w", err)
	}

	var availableCPU, availableMemory resource.Quantity
	var requestedPods, availablePods int64
	nodeHeadrooms := make([]NodeResourceHeadroom, 0, len(eligible))
	for _, node := range nodes {
		eligibleNode := eligible[node.Name]
		if eligibleNode == nil {
			continue
		}
		nodeUsage := usage[node.Name]
		cpu := eligibleNode.Status.Allocatable[corev1.ResourceCPU].DeepCopy()
		cpu.Sub(nodeUsage.cpu)
		memory := eligibleNode.Status.Allocatable[corev1.ResourceMemory].DeepCopy()
		memory.Sub(nodeUsage.memory)
		nodePods, podCapacityKnown := eligibleNode.Status.Allocatable[corev1.ResourcePods]
		nodeAvailablePods := int64(0)
		if podCapacityKnown {
			nodeAvailablePods = nodePods.Value() - nodeUsage.pods
		}
		availableCPU.Add(cpu)
		availableMemory.Add(memory)
		requestedPods += nodeUsage.pods
		availablePods += nodeAvailablePods
		nodeHeadrooms = append(nodeHeadrooms, NodeResourceHeadroom{
			Name:             node.Name,
			AvailableCPU:     cpu,
			AvailableMemory:  memory,
			AvailablePods:    nodeAvailablePods,
			PodCapacityKnown: podCapacityKnown,
		})
	}

	return &ClusterResourceHeadroom{
		NodeCapacity:     nodeCapacity,
		RequestedCPU:     requestedCPU,
		RequestedMemory:  requestedMemory,
		AvailableCPU:     availableCPU,
		AvailableMemory:  availableMemory,
		RequestedPods:    requestedPods,
		AvailablePods:    availablePods,
		PodCapacityKnown: nodeCapacity.PodCapacityKnown,
		NodeHeadrooms:    nodeHeadrooms,
	}, nil
}

// UnmodeledPodSchedulingConstraints lists hard scheduling constraints that
// cannot be evaluated from the Node list and PodSpec alone. A non-empty result
// means CPU/memory headroom must be reported as unknown rather than Safe.
func UnmodeledPodSchedulingConstraints(podSpec *corev1.PodSpec) []string { //nolint:gocyclo // Each Kubernetes scheduling feature is reported independently.
	if podSpec == nil {
		return nil
	}
	materialized := materializePodSpec(*podSpec)
	podSpec = &materialized
	var constraints []string
	if podSpec.NodeName != "" {
		constraints = append(constraints, "fixed nodeName")
	}
	if hasHardAffinity(podSpec.Affinity) {
		constraints = append(constraints, "required affinity/anti-affinity")
	}
	for _, constraint := range podSpec.TopologySpreadConstraints {
		if constraint.WhenUnsatisfiable == corev1.DoNotSchedule {
			constraints = append(constraints, "hard topology spread")
			break
		}
	}
	if podSpec.SchedulerName != "" && podSpec.SchedulerName != corev1.DefaultSchedulerName {
		constraints = append(constraints, "custom scheduler")
	}
	if podSpec.RuntimeClassName != nil && *podSpec.RuntimeClassName != "" {
		constraints = append(constraints, "RuntimeClass scheduling")
	}
	if len(podSpec.SchedulingGates) > 0 {
		constraints = append(constraints, "scheduling gates")
	}
	if len(podSpec.ResourceClaims) > 0 {
		constraints = append(constraints, "dynamic resource claims")
	}
	if podSpec.OS != nil {
		constraints = append(constraints, "Pod OS")
	}
	if podUsesPersistentVolumeClaim(podSpec.Volumes) {
		constraints = append(constraints, "persistent-volume node affinity")
	}
	if podUsesHostPorts(podSpec) {
		constraints = append(constraints, "host ports")
	}
	if podRequestsUnmodeledResources(podSpec) {
		constraints = append(constraints, "non-CPU/memory resources")
	}
	return constraints
}

func hasHardAffinity(affinity *corev1.Affinity) bool {
	if affinity == nil {
		return false
	}
	return (affinity.NodeAffinity != nil && affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil) ||
		(affinity.PodAffinity != nil && len(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0) ||
		(affinity.PodAntiAffinity != nil && len(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0)
}

func podUsesPersistentVolumeClaim(volumes []corev1.Volume) bool {
	for i := range volumes {
		if volumes[i].PersistentVolumeClaim != nil || volumes[i].Ephemeral != nil || volumes[i].CSI != nil {
			return true
		}
	}
	return false
}

func podUsesHostPorts(podSpec *corev1.PodSpec) bool {
	for i := range podSpec.Containers {
		for _, port := range podSpec.Containers[i].Ports {
			if port.HostPort != 0 || (podSpec.HostNetwork && port.ContainerPort != 0) {
				return true
			}
		}
	}
	for i := range podSpec.InitContainers {
		for _, port := range podSpec.InitContainers[i].Ports {
			if port.HostPort != 0 || (podSpec.HostNetwork && port.ContainerPort != 0) {
				return true
			}
		}
	}
	return false
}

func podRequestsUnmodeledResources(podSpec *corev1.PodSpec) bool {
	hasUnmodeled := func(resources corev1.ResourceList) bool {
		for name, quantity := range resources {
			if quantity.IsZero() {
				continue
			}
			if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
				return true
			}
		}
		return false
	}
	for i := range podSpec.Containers {
		if hasUnmodeled(podSpec.Containers[i].Resources.Requests) {
			return true
		}
	}
	for i := range podSpec.InitContainers {
		if hasUnmodeled(podSpec.InitContainers[i].Resources.Requests) {
			return true
		}
	}
	if podSpec.Resources != nil && hasUnmodeled(podSpec.Resources.Requests) {
		return true
	}
	return hasUnmodeled(podSpec.Overhead)
}
