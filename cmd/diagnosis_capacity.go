package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func buildCapacityHeadroomWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string, snapshot *observation.Snapshot) *hpaanalysis.CapacityHeadroom {
	if client == nil || hpa == nil {
		return nil
	}
	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	targetObservation := snapshot.ScaleTarget(ctx)
	if !targetObservation.Known() || targetObservation.Data.PodTemplate == nil {
		return nil
	}
	info := targetObservation.Data
	cpuPerPod, memPerPod := sumPodTemplateRequests(info.PodTemplate)
	additional := hpa.Spec.MaxReplicas - hpa.Status.DesiredReplicas
	if additional < 0 {
		additional = 0
	}
	addCPU := multiplyQuantity(cpuPerPod, additional)
	addMem := multiplyQuantity(memPerPod, additional)
	headroom := &hpaanalysis.CapacityHeadroom{
		HPAName:                    hpa.Name,
		Target:                     target,
		MaxReplicas:                hpa.Spec.MaxReplicas,
		CurrentDesired:             hpa.Status.DesiredReplicas,
		AdditionalReplicasToMax:    additional,
		PodRequestCPU:              quantityOrEmpty(cpuPerPod),
		PodRequestMemory:           quantityOrEmpty(memPerPod),
		AdditionalCPUToMax:         quantityOrEmpty(addCPU),
		AdditionalMemoryToMax:      quantityOrEmpty(addMem),
		ClusterSchedulableHeadroom: "unknown",
		Risk:                       "cluster schedulable headroom could not be confirmed from visible API data",
	}
	clusterHeadroom, headroomErr := kube.FetchClusterResourceHeadroom(ctx, client.Interface)
	if headroomErr != nil {
		headroom.Evidence = append(headroom.Evidence, fmt.Sprintf("cluster request headroom unavailable: %v", headroomErr))
	}
	if headroomErr == nil && clusterHeadroom != nil {
		assessClusterHeadroom(headroom, clusterHeadroom, addCPU, addMem, additional)
	}
	return headroom
}

// assessClusterHeadroom records node/pod evidence on the headroom report and
// classifies whether the cluster can schedule the additional replicas.
func assessClusterHeadroom(headroom *hpaanalysis.CapacityHeadroom, cluster *kube.ClusterResourceHeadroom, addCPU, addMem resource.Quantity, additional int32) {
	nodeCap := cluster.NodeCapacity
	headroom.Evidence = append(headroom.Evidence,
		fmt.Sprintf("nodes=%d allocatable cpu=%s memory=%s", nodeCap.TotalNodes, nodeCap.AllocCPU.String(), nodeCap.AllocMemory.String()),
		fmt.Sprintf("scheduled pod requests cpu=%s memory=%s", cluster.RequestedCPU.String(), cluster.RequestedMemory.String()),
		fmt.Sprintf("remaining request headroom cpu=%s memory=%s", cluster.AvailableCPU.String(), cluster.AvailableMemory.String()),
	)
	switch {
	case additional == 0:
		headroom.ClusterSchedulableHeadroom = "none needed"
		headroom.Risk = "HPA desiredReplicas is already at or above maxReplicas"
	case quantityAtLeast(cluster.AvailableCPU, addCPU) && quantityAtLeast(cluster.AvailableMemory, addMem):
		headroom.ClusterSchedulableHeadroom = "available"
		headroom.Risk = "visible node allocatable request headroom appears sufficient; scheduler constraints may still apply"
	default:
		headroom.ClusterSchedulableHeadroom = "low"
		headroom.Risk = "HPA can request more Pods, but Pods may stay Pending"
	}
}

func sumPodTemplateRequests(tmpl *corev1.PodTemplateSpec) (resource.Quantity, resource.Quantity) {
	var cpu, mem resource.Quantity
	if tmpl == nil {
		return cpu, mem
	}
	requests := kube.EffectivePodRequests(tmpl.Spec)
	if q, ok := requests[corev1.ResourceCPU]; ok {
		cpu = q.DeepCopy()
	}
	if q, ok := requests[corev1.ResourceMemory]; ok {
		mem = q.DeepCopy()
	}
	return cpu, mem
}

func multiplyQuantity(q resource.Quantity, factor int32) resource.Quantity {
	out := q.DeepCopy()
	if factor <= 0 || q.IsZero() {
		return resource.Quantity{}
	}
	out.SetMilli(q.MilliValue() * int64(factor))
	return out
}

func quantityOrEmpty(q resource.Quantity) string {
	if q.IsZero() {
		return ""
	}
	return q.String()
}

func quantityAtLeast(have, need resource.Quantity) bool {
	return need.IsZero() || have.Cmp(need) >= 0
}
