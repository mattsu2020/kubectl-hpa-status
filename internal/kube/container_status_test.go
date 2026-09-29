package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainerStatusesFromPods(t *testing.T) {
	t.Run("empty selector", func(t *testing.T) {
		result := ContainerStatusesFromPods(nil)
		if result != nil {
			t.Errorf("expected nil for empty pod set, got %v", result)
		}
	})

	t.Run("running pods with no issues", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-abc",
				Namespace: "default",
				Labels:    map[string]string{"app": "web"},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: true,
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{},
						},
					},
				},
			},
		}
		result := ContainerStatusesFromPods([]corev1.Pod{*pod})
		if len(result) != 1 {
			t.Fatalf("expected 1 container status, got %d", len(result))
		}
		if result[0].Waiting {
			t.Error("expected Waiting=false for running container")
		}
	})

	t.Run("pod with ImagePullBackOff", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-abc",
				Namespace: "default",
				Labels:    map[string]string{"app": "web"},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason: "ImagePullBackOff",
							},
						},
					},
				},
			},
		}
		result := ContainerStatusesFromPods([]corev1.Pod{*pod})
		if len(result) != 1 {
			t.Fatalf("expected 1 container status, got %d", len(result))
		}
		if !result[0].Waiting {
			t.Error("expected Waiting=true for ImagePullBackOff container")
		}
		if result[0].WaitingReason != "ImagePullBackOff" {
			t.Errorf("expected WaitingReason=ImagePullBackOff, got %s", result[0].WaitingReason)
		}
	})

	t.Run("pod with CrashLoopBackOff", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-abc",
				Namespace: "default",
				Labels:    map[string]string{"app": "web"},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "app",
						Ready:        false,
						RestartCount: 5,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason: "CrashLoopBackOff",
							},
						},
					},
				},
			},
		}
		result := ContainerStatusesFromPods([]corev1.Pod{*pod})
		if len(result) != 1 {
			t.Fatalf("expected 1 container status, got %d", len(result))
		}
		if result[0].RestartCount != 5 {
			t.Errorf("expected RestartCount=5, got %d", result[0].RestartCount)
		}
	})
}
