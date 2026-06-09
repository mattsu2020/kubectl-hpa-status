package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildRolloutDiagnosis(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.RolloutDiagnosis {
	if client == nil || hpa == nil {
		return nil
	}
	ref := hpa.Spec.ScaleTargetRef
	switch ref.Kind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		diag := &hpaanalysis.RolloutDiagnosis{
			Kind:                "Deployment",
			Name:                deploy.Name,
			DesiredReplicas:     replicasOrDefault(deploy.Spec.Replicas),
			UpdatedReplicas:     deploy.Status.UpdatedReplicas,
			ReadyReplicas:       deploy.Status.ReadyReplicas,
			AvailableReplicas:   deploy.Status.AvailableReplicas,
			UnavailableReplicas: deploy.Status.UnavailableReplicas,
		}
		diag.InProgress = deploymentRolloutInProgress(deploy)
		for _, condition := range deploy.Status.Conditions {
			diag.Conditions = append(diag.Conditions, fmt.Sprintf("%s=%s reason=%s", condition.Type, condition.Status, condition.Reason))
		}
		fillRolloutReasonAndPods(ctx, client, hpa, diag)
		return diag
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		diag := &hpaanalysis.RolloutDiagnosis{
			Kind:                "StatefulSet",
			Name:                sts.Name,
			DesiredReplicas:     replicasOrDefault(sts.Spec.Replicas),
			UpdatedReplicas:     sts.Status.UpdatedReplicas,
			ReadyReplicas:       sts.Status.ReadyReplicas,
			AvailableReplicas:   sts.Status.AvailableReplicas,
			UnavailableReplicas: sts.Status.Replicas - sts.Status.ReadyReplicas,
			InProgress:          sts.Status.UpdatedReplicas < replicasOrDefault(sts.Spec.Replicas) || sts.Status.ReadyReplicas < replicasOrDefault(sts.Spec.Replicas),
		}
		fillRolloutReasonAndPods(ctx, client, hpa, diag)
		return diag
	default:
		return nil
	}
}

func deploymentRolloutInProgress(deploy *appsv1.Deployment) bool {
	if deploy == nil {
		return false
	}
	desired := replicasOrDefault(deploy.Spec.Replicas)
	return deploy.Status.UpdatedReplicas < desired ||
		deploy.Status.AvailableReplicas < desired ||
		deploy.Status.UnavailableReplicas > 0 ||
		deploy.Generation != deploy.Status.ObservedGeneration
}

func fillRolloutReasonAndPods(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, diag *hpaanalysis.RolloutDiagnosis) {
	if diag == nil {
		return
	}
	if diag.InProgress {
		diag.Reason = "rollout in progress; new pods may not be Ready yet"
		diag.NextActions = append(diag.NextActions, "Inspect rollout status and new pod readiness before changing HPA thresholds.")
	}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil || info.SelectorStr == "" {
		return
	}
	pods, err := client.Interface.CoreV1().Pods(hpa.Namespace).List(ctx, metav1.ListOptions{LabelSelector: info.SelectorStr})
	if err != nil {
		return
	}
	for _, pod := range pods.Items {
		for _, cs := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
			if cs.State.Waiting == nil {
				continue
			}
			reason := cs.State.Waiting.Reason
			if reason == "ImagePullBackOff" || reason == "ErrImagePull" || reason == "CrashLoopBackOff" {
				diag.PodIssues = append(diag.PodIssues, fmt.Sprintf("%s/%s waiting: %s", pod.Name, cs.Name, reason))
			}
		}
		if pod.Status.Phase == corev1.PodPending {
			diag.PodIssues = append(diag.PodIssues, fmt.Sprintf("%s is Pending", pod.Name))
		}
	}
}

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
	usedCPU, usedMem := sumScheduledPodRequests(ctx, client)
	if nodeErr == nil && nodeCap != nil {
		cpuRemaining := nodeCap.AllocCPU.DeepCopy()
		cpuRemaining.Sub(usedCPU)
		memRemaining := nodeCap.AllocMemory.DeepCopy()
		memRemaining.Sub(usedMem)
		headroom.Evidence = append(headroom.Evidence,
			fmt.Sprintf("nodes=%d allocatable cpu=%s memory=%s", nodeCap.TotalNodes, nodeCap.AllocCPU.String(), nodeCap.AllocMemory.String()),
			fmt.Sprintf("scheduled pod requests cpu=%s memory=%s", usedCPU.String(), usedMem.String()),
			fmt.Sprintf("remaining request headroom cpu=%s memory=%s", cpuRemaining.String(), memRemaining.String()),
		)
		if additional == 0 {
			headroom.ClusterSchedulableHeadroom = "none needed"
			headroom.Risk = "HPA desiredReplicas is already at or above maxReplicas"
		} else if quantityAtLeast(cpuRemaining, addCPU) && quantityAtLeast(memRemaining, addMem) {
			headroom.ClusterSchedulableHeadroom = "available"
			headroom.Risk = "visible node allocatable request headroom appears sufficient; scheduler constraints may still apply"
		} else {
			headroom.ClusterSchedulableHeadroom = "low"
			headroom.Risk = "HPA can request more Pods, but Pods may stay Pending"
		}
	}
	return headroom
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

func sumScheduledPodRequests(ctx context.Context, client *kube.Client) (resource.Quantity, resource.Quantity) {
	var cpu, mem resource.Quantity
	pods, err := client.Interface.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return cpu, mem
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
