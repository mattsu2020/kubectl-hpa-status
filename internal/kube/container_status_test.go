package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchContainerStatuses(t *testing.T) {
	t.Run("empty selector", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		result, err := FetchContainerStatuses(context.Background(), client, "default", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil for empty selector, got %v", result)
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
		client := fakeClientWithPods(pod)
		result, err := FetchContainerStatuses(context.Background(), client, "default", "app=web")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
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
		client := fakeClientWithPods(pod)
		result, err := FetchContainerStatuses(context.Background(), client, "default", "app=web")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
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
		client := fakeClientWithPods(pod)
		result, err := FetchContainerStatuses(context.Background(), client, "default", "app=web")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 container status, got %d", len(result))
		}
		if result[0].RestartCount != 5 {
			t.Errorf("expected RestartCount=5, got %d", result[0].RestartCount)
		}
	})
}

func fakeClientWithPods(pods ...*corev1.Pod) *fake.Clientset {
	objects := make([]runtime.Object, 0, len(pods))
	for _, pod := range pods {
		objects = append(objects, pod)
	}
	return fake.NewSimpleClientset(objects...) //nolint:staticcheck // SA1019 deprecated, no replacement
}
