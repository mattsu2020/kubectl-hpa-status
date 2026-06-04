package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// AnalyzePods examines pods belonging to an HPA scale target for readiness,
// resource request completeness, and ContainerResource metric validity.
// Returns nil if the pod list is empty.
func AnalyzePods(pods []corev1.Pod, hpa *autoscalingv2.HorizontalPodAutoscaler) *PodAnalysis {
	if len(pods) == 0 {
		return nil
	}

	result := &PodAnalysis{
		Total: int32(len(pods)),
	}

	for i := range pods {
		pod := &pods[i]
		analyzePodPhase(pod, result)
		analyzePodContainerResources(pod, result)
	}

	result.ContainerChecks = validateContainerResourceMetrics(pods, hpa)

	return result
}

// analyzePodPhase counts pod phase states (ready, unready, pending, terminating).
func analyzePodPhase(pod *corev1.Pod, result *PodAnalysis) {
	switch pod.Status.Phase {
	case corev1.PodPending:
		result.Pending++
	case corev1.PodRunning:
		if isPodReady(pod) {
			result.Ready++
		} else {
			result.Unready++
		}
	case corev1.PodFailed:
		result.Unready++
	}

	if pod.DeletionTimestamp != nil {
		result.Terminating++
	}
}

// isPodReady checks if a pod has the Ready condition set to True.
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// analyzePodContainerResources checks each container for missing CPU/memory requests and limits.
func analyzePodContainerResources(pod *corev1.Pod, result *PodAnalysis) {
	for ci := range pod.Spec.Containers {
		container := &pod.Spec.Containers[ci]
		requests := container.Resources.Requests
		limits := container.Resources.Limits

		if requests.Cpu().IsZero() {
			result.ResourceIssues = append(result.ResourceIssues, PodResourceIssue{
				Pod:       pod.Name,
				Container: container.Name,
				Resource:  "cpu",
				Category:  "missing-request",
			})
		}
		if requests.Memory().IsZero() {
			result.ResourceIssues = append(result.ResourceIssues, PodResourceIssue{
				Pod:       pod.Name,
				Container: container.Name,
				Resource:  "memory",
				Category:  "missing-request",
			})
		}
		if limits.Cpu().IsZero() {
			result.ResourceIssues = append(result.ResourceIssues, PodResourceIssue{
				Pod:       pod.Name,
				Container: container.Name,
				Resource:  "cpu",
				Category:  "missing-limit",
			})
		}
		if limits.Memory().IsZero() {
			result.ResourceIssues = append(result.ResourceIssues, PodResourceIssue{
				Pod:       pod.Name,
				Container: container.Name,
				Resource:  "memory",
				Category:  "missing-limit",
			})
		}
	}
}

// validateContainerResourceMetrics checks that ContainerResource metric targets
// reference containers that actually exist in the pod template.
func validateContainerResourceMetrics(pods []corev1.Pod, hpa *autoscalingv2.HorizontalPodAutoscaler) []ContainerCheck {
	if len(pods) == 0 || hpa == nil {
		return nil
	}

	var checks []ContainerCheck
	seen := make(map[string]bool)

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ContainerResourceMetricSourceType {
			continue
		}
		if spec.ContainerResource == nil {
			continue
		}
		containerName := spec.ContainerResource.Container
		if seen[containerName] {
			continue
		}
		seen[containerName] = true

		found := false
		for _, pod := range pods {
			for _, container := range pod.Spec.Containers {
				if container.Name == containerName {
					found = true
					break
				}
			}
			if found {
				break
			}
		}

		check := ContainerCheck{
			Container: containerName,
			Found:     found,
		}
		if !found {
			check.Message = fmt.Sprintf("container %q referenced by ContainerResource metric not found in pod template", containerName)
		}
		checks = append(checks, check)
	}

	return checks
}
