package kube

import (
	"context"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFetchScaleTargetInfo_Deployment(t *testing.T) {
	cpuRequest := resource.MustParse("100m")
	cpuLimit := resource.MustParse("500m")

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
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: cpuLimit,
								},
							},
						},
					},
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 2,
		},
	}

	client := testutil.NewFakeClientWithObjects(deploy)
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "Deployment",
		Name: "web",
	}

	info, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Kind != "Deployment" {
		t.Fatalf("expected kind 'Deployment', got %q", info.Kind)
	}
	if info.Name != "web" {
		t.Fatalf("expected name 'web', got %q", info.Name)
	}
	if info.Namespace != "default" {
		t.Fatalf("expected namespace 'default', got %q", info.Namespace)
	}
	if info.Replicas != 3 {
		t.Fatalf("expected 3 replicas, got %d", info.Replicas)
	}
	if info.ReadyReplicas != 2 {
		t.Fatalf("expected 2 ready replicas, got %d", info.ReadyReplicas)
	}
	if info.SelectorStr != "app=web" {
		t.Fatalf("expected selector 'app=web', got %q", info.SelectorStr)
	}
	if info.PodTemplate == nil {
		t.Fatal("expected non-nil PodTemplate")
	}
	if len(info.PodTemplate.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(info.PodTemplate.Spec.Containers))
	}
}

func TestFetchScaleTargetInfo_StatefulSet(t *testing.T) {
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
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "db"},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      5,
			ReadyReplicas: 5,
		},
	}

	client := testutil.NewFakeClientWithObjects(sts)
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "StatefulSet",
		Name: "db",
	}

	info, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Kind != "StatefulSet" {
		t.Fatalf("expected kind 'StatefulSet', got %q", info.Kind)
	}
	if info.Replicas != 5 {
		t.Fatalf("expected 5 replicas, got %d", info.Replicas)
	}
	if info.SelectorStr != "app=db" {
		t.Fatalf("expected selector 'app=db', got %q", info.SelectorStr)
	}
}

func TestFetchScaleTargetInfo_ReplicaSet(t *testing.T) {
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
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:      2,
			ReadyReplicas: 1,
		},
	}

	client := testutil.NewFakeClientWithObjects(rs)
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "ReplicaSet",
		Name: "web-abc123",
	}

	info, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Kind != "ReplicaSet" {
		t.Fatalf("expected kind 'ReplicaSet', got %q", info.Kind)
	}
	if info.Replicas != 2 {
		t.Fatalf("expected 2 replicas, got %d", info.Replicas)
	}
	if info.ReadyReplicas != 1 {
		t.Fatalf("expected 1 ready replica, got %d", info.ReadyReplicas)
	}
}

func TestFetchScaleTargetInfo_UnknownKind(t *testing.T) {
	client := testutil.NewFakeClientWithObjects()
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "DaemonSet",
		Name: "agent",
	}

	info, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatal("expected nil info for unsupported kind")
	}
}

func TestFetchScaleTargetInfo_DeploymentNotFound(t *testing.T) {
	client := testutil.NewFakeClientWithObjects()
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "Deployment",
		Name: "missing",
	}

	_, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err == nil {
		t.Fatal("expected error for missing deployment")
	}
}

func TestFetchScaleTargetInfo_NilSelector(t *testing.T) {
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
			Selector: nil,
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      1,
			ReadyReplicas: 1,
		},
	}

	client := testutil.NewFakeClientWithObjects(deploy)
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "Deployment",
		Name: "web",
	}

	info, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.SelectorStr != "<none>" {
		t.Fatalf("expected '<none>' selector string for nil selector, got %q", info.SelectorStr)
	}
}

func TestFetchScaleTargetInfo_MultipleContainers(t *testing.T) {
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
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      3,
			ReadyReplicas: 3,
		},
	}

	client := testutil.NewFakeClientWithObjects(deploy)
	ref := autoscalingv2.CrossVersionObjectReference{
		Kind: "Deployment",
		Name: "web",
	}

	info, err := FetchScaleTargetInfo(context.Background(), client, "default", ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if len(info.PodTemplate.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(info.PodTemplate.Spec.Containers))
	}
}
