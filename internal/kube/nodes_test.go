package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchNodeCapacity(t *testing.T) {
	t.Run("empty cluster", func(t *testing.T) {
		client := fakeClientWithNodes()
		summary, err := FetchNodeCapacity(context.Background(), client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summary.TotalNodes != 0 {
			t.Errorf("expected 0 nodes, got %d", summary.TotalNodes)
		}
	})

	t.Run("single node", func(t *testing.T) {
		client := fakeClientWithNodes(buildTestNode("node-1", "4", "16Gi", nil))
		summary, err := FetchNodeCapacity(context.Background(), client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summary.TotalNodes != 1 {
			t.Errorf("expected 1 node, got %d", summary.TotalNodes)
		}
		if summary.AllocCPU.String() != "4" {
			t.Errorf("expected 4 CPU, got %s", summary.AllocCPU.String())
		}
		if summary.TaintedNodes != 0 {
			t.Errorf("expected 0 tainted nodes, got %d", summary.TaintedNodes)
		}
	})

	t.Run("node with taints", func(t *testing.T) {
		taints := []corev1.Taint{
			{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
		}
		client := fakeClientWithNodes(
			buildTestNode("node-1", "4", "16Gi", taints),
			buildTestNode("node-2", "8", "32Gi", nil),
		)
		summary, err := FetchNodeCapacity(context.Background(), client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summary.TotalNodes != 2 {
			t.Errorf("expected 2 nodes, got %d", summary.TotalNodes)
		}
		if summary.TaintedNodes != 1 {
			t.Errorf("expected 1 tainted node, got %d", summary.TaintedNodes)
		}
	})
}

func buildTestNode(name, cpu, memory string, taints []corev1.Taint) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.NodeSpec{
			Taints: taints,
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
			},
		},
	}
}

func fakeClientWithNodes(nodes ...*corev1.Node) *fake.Clientset {
	objects := make([]runtime.Object, 0, len(nodes))
	for _, node := range nodes {
		objects = append(objects, node)
	}
	return fake.NewSimpleClientset(objects...) //nolint:staticcheck // SA1019 deprecated, no replacement
}
