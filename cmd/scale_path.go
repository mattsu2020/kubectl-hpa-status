package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/containeradvisor"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// This file holds the scale-path and container-advisor builders that feed
// per-HPA enrichment. They are kept separate from status.go's multi-HPA
// orchestration so the command wiring and the per-feature data gathering
// evolve independently.

// buildScalePathWithSnapshot gathers pods, ReplicaSets, and events around the
// HPA's scale target and hands them to hpaanalysis.AnalyzeScalePath for
// diagnosis, reusing the caller's observation snapshot so the enrichment
// pipeline issues each API read once.
func buildScalePathWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, snapshot *observation.Snapshot) *hpaanalysis.ScalePath {
	input := hpaanalysis.ScalePathInput{}
	var collectionWarnings []string
	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	target := snapshot.ScaleTarget(ctx)
	if target.State == observation.StateUnavailable {
		collectionWarnings = append(collectionWarnings, fmt.Sprintf("scale target unavailable: %v", target.Err))
	}
	if target.Known() {
		info := target.Data
		input.Target = &hpaanalysis.ScalePathTarget{
			Kind:            info.Kind,
			Name:            info.Name,
			DesiredReplicas: info.DesiredReplicas,
			CurrentReplicas: info.Replicas,
			ReadyReplicas:   info.ReadyReplicas,
		}
		pods := snapshot.PodInfos(ctx)
		switch pods.State {
		case observation.StateKnown:
			input.Pods = convertScalePathPods(pods.Data)
		case observation.StateUnavailable:
			collectionWarnings = append(collectionWarnings, fmt.Sprintf("pods unavailable: %v", pods.Err))
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

func fetchTargetReplicaInfoFromSnapshot(ctx context.Context, snapshot *observation.Snapshot, _ *autoscalingv2.HorizontalPodAutoscaler) (*hpaanalysis.TargetReplicaInfo, string) {
	if snapshot == nil {
		return nil, "target replica observation unavailable: observation snapshot is not configured"
	}
	target := snapshot.ScaleTarget(ctx)
	if target.State == observation.StateUnavailable {
		return nil, fmt.Sprintf("target replica observation unavailable: %v", target.Err)
	}
	if !target.Known() {
		return nil, ""
	}
	info := target.Data
	notReady := info.Replicas - info.ReadyReplicas
	result := &hpaanalysis.TargetReplicaInfo{
		TotalReplicas: info.Replicas,
		ReadyReplicas: info.ReadyReplicas,
		NotReady:      notReady,
	}
	pods := snapshot.PodInfos(ctx)
	if pods.State == observation.StateUnavailable {
		return result, fmt.Sprintf("target Pod observation unavailable: %v", pods.Err)
	}
	if pods.Known() {
		for _, pod := range pods.Data {
			if pod.Phase == "Pending" {
				result.Pending++
				if pod.Unschedulable {
					result.Unschedulable++
				}
			}
		}
	}
	if result.NotReady <= 0 && result.Pending <= 0 && result.Unschedulable <= 0 {
		return nil, ""
	}
	return result, ""
}

// podUnschedulable is retained as the command-layer compatibility helper used
// by existing callers and tests. Request-scoped analysis derives the same
// signal from observation.Snapshot without another Pod list.
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
