package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchScaleTargetResources_Deployment(t *testing.T) {
	cpuRequest := resource.MustParse("100m")
	cpuLimit := resource.MustParse("500m")
	memRequest := resource.MustParse("128Mi")

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    cpuRequest,
									corev1.ResourceMemory: memRequest,
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: cpuLimit,
								},
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewClientset(deploy)
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "Deployment", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(result.Containers))
	}
	if result.Containers[0].Name != "app" {
		t.Fatalf("expected container name 'app', got %q", result.Containers[0].Name)
	}
	if result.Containers[0].Requests["cpu"] != "100m" {
		t.Fatalf("expected cpu request '100m', got %q", result.Containers[0].Requests["cpu"])
	}
	if result.Containers[0].Requests["memory"] != "128Mi" {
		t.Fatalf("expected memory request '128Mi', got %q", result.Containers[0].Requests["memory"])
	}
	if result.Containers[0].Limits["cpu"] != "500m" {
		t.Fatalf("expected cpu limit '500m', got %q", result.Containers[0].Limits["cpu"])
	}
	if _, ok := result.Containers[0].Limits["memory"]; ok {
		t.Fatal("expected no memory limit")
	}
}

func TestFetchScaleTargetResources_StatefulSet(t *testing.T) {
	cpuRequest := resource.MustParse("200m")

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "db",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "database",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: cpuRequest,
								},
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewClientset(sts)
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "StatefulSet", "db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Containers[0].Name != "database" {
		t.Fatalf("expected container name 'database', got %q", result.Containers[0].Name)
	}
	if result.Containers[0].Requests["cpu"] != "200m" {
		t.Fatalf("expected cpu request '200m', got %q", result.Containers[0].Requests["cpu"])
	}
}

func TestFetchScaleTargetResources_ReplicaSet(t *testing.T) {
	cpuRequest := resource.MustParse("50m")

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-abc123",
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: cpuRequest,
								},
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewClientset(rs)
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "ReplicaSet", "web-abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Containers[0].Requests["cpu"] != "50m" {
		t.Fatalf("expected cpu request '50m', got %q", result.Containers[0].Requests["cpu"])
	}
}

func TestFetchScaleTargetResources_UnsupportedKind(t *testing.T) {
	client := fake.NewClientset()
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "DaemonSet", "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for unsupported kind")
	}
}

func TestFetchScaleTargetResources_DeploymentNotFound(t *testing.T) {
	client := fake.NewClientset()
	_, err := FetchScaleTargetResources(context.Background(), client, "default", "Deployment", "missing")
	if err == nil {
		t.Fatal("expected error for missing deployment")
	}
}

func TestFetchScaleTargetResources_MultipleContainers(t *testing.T) {
	cpuRequest := resource.MustParse("100m")
	sidecarMem := resource.MustParse("64Mi")

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: cpuRequest,
								},
							},
						},
						{
							Name: "sidecar",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: sidecarMem,
								},
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewClientset(deploy)
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "Deployment", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(result.Containers))
	}
	if result.Containers[0].Name != "app" {
		t.Fatalf("expected first container 'app', got %q", result.Containers[0].Name)
	}
	if result.Containers[1].Name != "sidecar" {
		t.Fatalf("expected second container 'sidecar', got %q", result.Containers[1].Name)
	}
}

func TestFetchScaleTargetResources_NoResources(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app"},
					},
				},
			},
		},
	}

	client := fake.NewClientset(deploy)
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "Deployment", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even with no resources set")
	}
	if len(result.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(result.Containers))
	}
	if len(result.Containers[0].Requests) != 0 {
		t.Fatalf("expected empty requests, got %v", result.Containers[0].Requests)
	}
	if len(result.Containers[0].Limits) != 0 {
		t.Fatalf("expected empty limits, got %v", result.Containers[0].Limits)
	}
}

func TestExtractResourcesFromPodTemplate_Nil(t *testing.T) {
	result := extractResourcesFromPodTemplate(nil)
	if result != nil {
		t.Fatal("expected nil for nil template")
	}
}

// Ensure the fake client works with runtime objects from apps/v1.
func TestFetchScaleTargetResources_EmptyContainers(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	}

	client := fake.NewClientset([]runtime.Object{deploy}...)
	result, err := FetchScaleTargetResources(context.Background(), client, "default", "Deployment", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for deployment with no containers")
	}
}
