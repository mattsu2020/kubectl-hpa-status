package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/containeradvisor"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This file holds the scale-path and container-advisor builders that feed
// per-HPA enrichment. They are kept separate from status.go's multi-HPA
// orchestration so the command wiring and the per-feature data gathering
// evolve independently.

// buildScalePath gathers pods, ReplicaSets, and events around the HPA's
// scale target and hands them to hpaanalysis.AnalyzeScalePath for diagnosis.
func buildScalePath(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.ScalePath {
	input := hpaanalysis.ScalePathInput{}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	var collectionWarnings []string
	if err != nil {
		collectionWarnings = append(collectionWarnings, fmt.Sprintf("scale target unavailable: %v", err))
	}
	if err == nil && info != nil {
		input.Target = &hpaanalysis.ScalePathTarget{
			Kind:            info.Kind,
			Name:            info.Name,
			DesiredReplicas: info.DesiredReplicas,
			CurrentReplicas: info.Replicas,
			ReadyReplicas:   info.ReadyReplicas,
		}
		if pods, podErr := kube.FetchPodInfosForSelector(ctx, client.Interface, hpa.Namespace, info.SelectorStr); podErr == nil {
			input.Pods = convertScalePathPods(pods)
		} else {
			collectionWarnings = append(collectionWarnings, fmt.Sprintf("pods unavailable: %v", podErr))
		}
		if replicaSets, rsErr := kube.FetchReplicaSetsForScaleTarget(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef, info.SelectorStr); rsErr == nil {
			input.ReplicaSets = convertScalePathReplicaSets(replicaSets)
		} else {
			collectionWarnings = append(collectionWarnings, fmt.Sprintf("replica sets unavailable: %v", rsErr))
		}
		objectNames := scalePathEventObjectNames(hpa, input.Pods, input.ReplicaSets)
		input.Events = convertScalePathEvents(kube.FetchRecentEventsForObjects(ctx, client.Interface, hpa.Namespace, objectNames, 10))
	}
	result := hpaanalysis.AnalyzeScalePath(hpa, input)
	result.ProbeWarnings = append(result.ProbeWarnings, collectionWarnings...)
	return result
}

func convertScalePathPods(pods []kube.PodInfo) []hpaanalysis.ScalePathPod {
	if len(pods) == 0 {
		return nil
	}
	result := make([]hpaanalysis.ScalePathPod, 0, len(pods))
	for _, pod := range pods {
		result = append(result, hpaanalysis.ScalePathPod{
			Name:          pod.Name,
			Phase:         pod.Phase,
			Ready:         pod.Ready,
			Unschedulable: pod.Unschedulable,
			Reasons:       pod.Reasons,
		})
	}
	return result
}

func convertScalePathReplicaSets(replicaSets []kube.ReplicaSetInfo) []hpaanalysis.ScalePathReplicaSet {
	if len(replicaSets) == 0 {
		return nil
	}
	result := make([]hpaanalysis.ScalePathReplicaSet, 0, len(replicaSets))
	for _, rs := range replicaSets {
		result = append(result, hpaanalysis.ScalePathReplicaSet{
			Name:            rs.Name,
			DesiredReplicas: rs.DesiredReplicas,
			CurrentReplicas: rs.CurrentReplicas,
			ReadyReplicas:   rs.ReadyReplicas,
		})
	}
	return result
}

func convertScalePathEvents(events []kube.EventInfo) []hpaanalysis.Event {
	if len(events) == 0 {
		return nil
	}
	result := make([]hpaanalysis.Event, 0, len(events))
	for _, event := range events {
		result = append(result, hpaanalysis.Event{
			Reason:    event.Reason,
			Message:   event.Message,
			Timestamp: event.Timestamp,
		})
	}
	return result
}

func scalePathEventObjectNames(hpa *autoscalingv2.HorizontalPodAutoscaler, pods []hpaanalysis.ScalePathPod, replicaSets []hpaanalysis.ScalePathReplicaSet) []string {
	names := []string{hpa.Name, hpa.Spec.ScaleTargetRef.Name}
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	for _, rs := range replicaSets {
		names = append(names, rs.Name)
	}
	return names
}

func fetchTargetReplicaInfo(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.TargetReplicaInfo {
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil {
		return nil
	}

	notReady := info.Replicas - info.ReadyReplicas
	result := &hpaanalysis.TargetReplicaInfo{
		TotalReplicas: info.Replicas,
		ReadyReplicas: info.ReadyReplicas,
		NotReady:      notReady,
	}
	enrichPendingPods(ctx, client, hpa.Namespace, info.SelectorStr, result)
	if result.NotReady <= 0 && result.Pending <= 0 && result.Unschedulable <= 0 {
		return nil
	}
	return result
}

func enrichPendingPods(ctx context.Context, client *kube.Client, namespace string, selector string, info *hpaanalysis.TargetReplicaInfo) {
	if selector == "" || info == nil {
		return
	}
	pods, err := client.Interface.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending {
			info.Pending++
			if podUnschedulable(pod) {
				info.Unschedulable++
			}
		}
	}
}

func podUnschedulable(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == corev1.PodReasonUnschedulable {
			return true
		}
	}
	return false
}

// buildContainerAdvisor builds the ContainerResource advisor result.
func buildContainerAdvisor(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *containeradvisor.Result {
	resources, err := kube.FetchScaleTargetResources(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	if err != nil || resources == nil {
		return nil
	}

	containerCount := len(resources.Containers)
	var containerNames []string
	for _, c := range resources.Containers {
		containerNames = append(containerNames, c.Name)
	}

	usesResource := false
	usesContainerResource := false
	for _, spec := range hpa.Spec.Metrics {
		switch spec.Type {
		case autoscalingv2.ResourceMetricSourceType:
			usesResource = true
		case autoscalingv2.ContainerResourceMetricSourceType:
			usesContainerResource = true
		}
	}

	input := containeradvisor.Input{
		ContainerCount:              containerCount,
		ContainerNames:              containerNames,
		UsesResourceMetric:          usesResource,
		UsesContainerResourceMetric: usesContainerResource,
	}

	return containeradvisor.Analyze(hpa, input)
}
