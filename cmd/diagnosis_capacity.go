package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildCapacityHeadroom(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string) *hpaanalysis.CapacityHeadroom {
	if client == nil || hpa == nil {
		return nil
	}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil || info.PodTemplate == nil {
		return nil
	}
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
	nodeCap, nodeErr := kube.FetchNodeCapacity(ctx, client.Interface)
	usedCPU, usedMem, podErr := sumScheduledPodRequests(ctx, client)
	if nodeErr != nil {
		headroom.Evidence = append(headroom.Evidence, fmt.Sprintf("node capacity unavailable: %v", nodeErr))
	}
	if podErr != nil {
		headroom.Evidence = append(headroom.Evidence, fmt.Sprintf("scheduled pod requests unavailable: %v", podErr))
	}
	if nodeErr == nil && podErr == nil && nodeCap != nil {
		assessClusterHeadroom(headroom, nodeCap, usedCPU, usedMem, addCPU, addMem, additional)
	}
	return headroom
}

// assessClusterHeadroom records node/pod evidence on the headroom report and
// classifies whether the cluster can schedule the additional replicas.
func assessClusterHeadroom(headroom *hpaanalysis.CapacityHeadroom, nodeCap *kube.NodeCapacityInfo, usedCPU, usedMem, addCPU, addMem resource.Quantity, additional int32) {
	cpuRemaining := nodeCap.AllocCPU.DeepCopy()
	cpuRemaining.Sub(usedCPU)
	memRemaining := nodeCap.AllocMemory.DeepCopy()
	memRemaining.Sub(usedMem)
	headroom.Evidence = append(headroom.Evidence,
		fmt.Sprintf("nodes=%d allocatable cpu=%s memory=%s", nodeCap.TotalNodes, nodeCap.AllocCPU.String(), nodeCap.AllocMemory.String()),
		fmt.Sprintf("scheduled pod requests cpu=%s memory=%s", usedCPU.String(), usedMem.String()),
		fmt.Sprintf("remaining request headroom cpu=%s memory=%s", cpuRemaining.String(), memRemaining.String()),
	)
	switch {
	case additional == 0:
		headroom.ClusterSchedulableHeadroom = "none needed"
		headroom.Risk = "HPA desiredReplicas is already at or above maxReplicas"
	case quantityAtLeast(cpuRemaining, addCPU) && quantityAtLeast(memRemaining, addMem):
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
	for _, container := range tmpl.Spec.Containers {
		if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			cpu.Add(q)
		}
		if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			mem.Add(q)
		}
	}
	return cpu, mem
}

func sumScheduledPodRequests(ctx context.Context, client *kube.Client) (resource.Quantity, resource.Quantity, error) {
	var cpu, mem resource.Quantity
	listOptions := metav1.ListOptions{Limit: 500}
	for {
		pods, err := client.Interface.CoreV1().Pods("").List(ctx, listOptions)
		if err != nil {
			return cpu, mem, fmt.Errorf("list cluster pods: %w", err)
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			for _, container := range pod.Spec.Containers {
				if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
					cpu.Add(q)
				}
				if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
					mem.Add(q)
				}
			}
		}
		if pods.Continue == "" {
			break
		}
		listOptions.Continue = pods.Continue
	}
	return cpu, mem, nil
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
