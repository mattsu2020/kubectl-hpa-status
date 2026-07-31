package kube

import (
	"context"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFetchClusterResourceHeadroom_SubtractsAllScheduledPodRequests(t *testing.T) {
	node := buildTestNode("node-1", "4", "8Gi", nil)
	scheduled := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "busy"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				}},
			}},
			InitContainers: []corev1.Container{{
				Name: "init",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				}},
			}},
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	unscheduled := scheduled.DeepCopy()
	unscheduled.Name = "pending"
	unscheduled.Spec.NodeName = ""
	terminal := scheduled.DeepCopy()
	terminal.Name = "done"
	terminal.Status.Phase = corev1.PodSucceeded
	client := testutil.NewFakeClientWithObjects(node, scheduled, unscheduled, terminal)

	headroom, err := FetchClusterResourceHeadroom(context.Background(), client)
	if err != nil {
		t.Fatalf("FetchClusterResourceHeadroom: %v", err)
	}

	if headroom.RequestedCPU.Cmp(resource.MustParse("3100m")) != 0 {
		t.Fatalf("requested CPU = %s, want 3100m", headroom.RequestedCPU.String())
	}
	if headroom.RequestedMemory.Cmp(resource.MustParse("4224Mi")) != 0 {
		t.Fatalf("requested memory = %s, want 4224Mi", headroom.RequestedMemory.String())
	}
	if headroom.AvailableCPU.Cmp(resource.MustParse("900m")) != 0 {
		t.Fatalf("available CPU = %s, want 900m", headroom.AvailableCPU.String())
	}
	if headroom.AvailableMemory.Cmp(resource.MustParse("3968Mi")) != 0 {
		t.Fatalf("available memory = %s, want 3968Mi", headroom.AvailableMemory.String())
	}
}

func TestFetchClusterResourceHeadroomPreservesPerNodeFragmentation(t *testing.T) {
	nodeA := buildTestNode("node-a", "1", "1Gi", nil)
	nodeB := buildTestNode("node-b", "1", "1Gi", nil)
	pod := func(name, nodeName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
			Spec: corev1.PodSpec{
				NodeName: nodeName,
				Containers: []corev1.Container{{
					Name: "app",
					Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					}},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	client := testutil.NewFakeClientWithObjects(
		nodeA,
		nodeB,
		pod("busy-a", "node-a"),
		pod("busy-b", "node-b"),
	)

	headroom, err := FetchClusterResourceHeadroom(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(headroom.NodeHeadrooms) != 2 {
		t.Fatalf("node headrooms = %+v", headroom.NodeHeadrooms)
	}
	for _, node := range headroom.NodeHeadrooms {
		if node.AvailableCPU.Cmp(resource.MustParse("500m")) != 0 {
			t.Fatalf("%s available CPU = %s, want 500m", node.Name, node.AvailableCPU.String())
		}
	}
}
