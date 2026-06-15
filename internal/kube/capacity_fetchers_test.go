package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchLimitRanges_NoLimitRanges(t *testing.T) {
	client := fake.NewSimpleClientset()
	result, err := FetchLimitRanges(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("FetchLimitRanges: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected no limit ranges, got %d", len(result))
	}
}

func TestFetchLimitRanges_WithConstraints(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().LimitRanges("default").Create(context.Background(), &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: "resource-limits"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Min: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create limitrange: %v", err)
	}

	result, err := FetchLimitRanges(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("FetchLimitRanges: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 constraints (cpu + memory), got %d", len(result))
	}

	foundCPU := false
	foundMemory := false
	for _, lr := range result {
		if lr.Resource == "cpu" && lr.Min == "100m" && lr.Max == "2" {
			foundCPU = true
		}
		if lr.Resource == "memory" && lr.Min == "128Mi" && lr.Max == "4Gi" {
			foundMemory = true
		}
	}
	if !foundCPU {
		t.Error("expected CPU constraint with min=100m, max=2")
	}
	if !foundMemory {
		t.Error("expected memory constraint with min=128Mi, max=4Gi")
	}
}

func TestFetchLimitRanges_MaxOnly(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().LimitRanges("default").Create(context.Background(), &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: "max-only"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Max: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("4"),
					},
				},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create limitrange: %v", err)
	}

	result, err := FetchLimitRanges(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("FetchLimitRanges: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(result))
	}
	if result[0].Min != "" {
		t.Errorf("expected empty min, got %q", result[0].Min)
	}
	if result[0].Max != "4" {
		t.Errorf("expected max '4', got %q", result[0].Max)
	}
}

func TestFetchAllResourceQuotas_ReturnsAll(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().ResourceQuotas("default").Create(context.Background(), &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "compute"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("10"),
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("10"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("3"),
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create resourcequota: %v", err)
	}

	result, err := FetchAllResourceQuotas(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("FetchAllResourceQuotas: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(result))
	}
	// Even though ratio is 0.3 (below 80%), FetchAll should return it.
	if result[0].Resource != "cpu" {
		t.Errorf("expected resource 'cpu', got %q", result[0].Resource)
	}
	if result[0].Used != "3" {
		t.Errorf("expected used '3', got %q", result[0].Used)
	}
}

func TestFetchAllResourceQuotas_None(t *testing.T) {
	client := fake.NewSimpleClientset()
	result, err := FetchAllResourceQuotas(context.Background(), client, "default")
	if err != nil {
		t.Fatalf("FetchAllResourceQuotas: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected no quotas, got %d", len(result))
	}
}

func TestDetectClusterAutoscaler_WithNodeAnnotation(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().Nodes().Create(context.Background(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Annotations: map[string]string{
				"cluster-autoscaler.kubernetes.io/safe-to-evict": "true",
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	result := DetectClusterAutoscaler(context.Background(), client)
	if !result {
		t.Error("expected Cluster Autoscaler to be detected via node annotation")
	}
}

func TestDetectClusterAutoscaler_WithDeployment(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.AppsV1().Deployments("kube-system").Create(context.Background(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-autoscaler"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "cluster-autoscaler"},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create deployment: %v", err)
	}

	result := DetectClusterAutoscaler(context.Background(), client)
	if !result {
		t.Error("expected Cluster Autoscaler to be detected via deployment")
	}
}

func TestDetectClusterAutoscaler_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	// Create a node without the annotation.
	if _, err := client.CoreV1().Nodes().Create(context.Background(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	result := DetectClusterAutoscaler(context.Background(), client)
	if result {
		t.Error("expected Cluster Autoscaler NOT to be detected")
	}
}
