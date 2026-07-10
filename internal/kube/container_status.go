package kube

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
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

// FetchContainerStatuses lists pods matching the selector and extracts
// container-level status information used for blocker detection.
func FetchContainerStatuses(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]ContainerStatusDetail, error) {
	if selector == "" {
		return nil, nil
	}

	pods, err := FetchPodObjectsForSelector(ctx, client, namespace, selector)
	if err != nil {
		return nil, err
	}
	return ContainerStatusesFromPods(pods), nil
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
