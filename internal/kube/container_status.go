package kube

import (
	corev1 "k8s.io/api/core/v1"
)

// ContainerStatusDetail holds container-level status information for blocker
// detection (ImagePullBackOff, CrashLoopBackOff, etc.).
type ContainerStatusDetail struct {
	Pod           string
	Container     string
	Waiting       bool
	WaitingReason string
	RestartCount  int32
}

// ContainerStatusesFromPods extracts container state from an already-fetched
// pod set.
func ContainerStatusesFromPods(pods []corev1.Pod) []ContainerStatusDetail {
	var result []ContainerStatusDetail
	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			detail := ContainerStatusDetail{
				Pod:          pod.Name,
				Container:    cs.Name,
				RestartCount: cs.RestartCount,
			}
			if cs.State.Waiting != nil {
				detail.Waiting = true
				detail.WaitingReason = cs.State.Waiting.Reason
			}
			result = append(result, detail)
		}
	}
	return result
}
