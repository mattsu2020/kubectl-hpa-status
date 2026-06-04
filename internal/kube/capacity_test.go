package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchPendingPodDetails_NoPending(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Labels: map[string]string{"app": "web"}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	result := FetchPendingPodDetails(context.Background(), client, "default", "app=web")

	if len(result) != 0 {
		t.Errorf("expected no pending pods, got %d", len(result))
	}
}

func TestFetchPendingPodDetails_WithPending(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := client.CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-pending", Labels: map[string]string{"app": "web"}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Reason:  corev1.PodReasonUnschedulable,
					Message: "Insufficient cpu",
				},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	result := FetchPendingPodDetails(context.Background(), client, "default", "app=web")

	if len(result) != 1 {
		t.Fatalf("expected 1 pending pod, got %d", len(result))
	}
	if result[0].Name != "pod-pending" {
		t.Errorf("expected name 'pod-pending', got %q", result[0].Name)
	}
	if !result[0].Unschedulable {
		t.Error("expected pod to be unschedulable")
	}
	if len(result[0].Reasons) != 1 || result[0].Reasons[0] != "Insufficient cpu" {
		t.Errorf("expected reason 'Insufficient cpu', got %v", result[0].Reasons)
	}
}

func TestFetchPendingPodDetails_EmptySelector(t *testing.T) {
	client := fake.NewSimpleClientset()
	result := FetchPendingPodDetails(context.Background(), client, "default", "")
	if result != nil {
		t.Errorf("expected nil for empty selector, got %v", result)
	}
}

func TestFetchResourceQuotas_None(t *testing.T) {
	client := fake.NewSimpleClientset()
	result := FetchResourceQuotas(context.Background(), client, "default")
	if len(result) != 0 {
		t.Errorf("expected no quotas, got %d", len(result))
	}
}

func TestFetchResourceQuotas_NearLimit(t *testing.T) {
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
				corev1.ResourceCPU: resource.MustParse("9"),
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create resourcequota: %v", err)
	}

	result := FetchResourceQuotas(context.Background(), client, "default")

	if len(result) != 1 {
		t.Fatalf("expected 1 quota constraint, got %d", len(result))
	}
	if result[0].Name != "compute" {
		t.Errorf("expected name 'compute', got %q", result[0].Name)
	}
	if result[0].Resource != "cpu" {
		t.Errorf("expected resource 'cpu', got %q", result[0].Resource)
	}
}

func TestFetchResourceQuotas_BelowThreshold(t *testing.T) {
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

	result := FetchResourceQuotas(context.Background(), client, "default")
	if len(result) != 0 {
		t.Errorf("expected no constraints below 80%%, got %d", len(result))
	}
}

func TestGenerateNodeHints(t *testing.T) {
	pending := []PendingPodDetail{
		{Name: "pod-1", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
	}
	quotas := []QuotaInfo{
		{Name: "compute", Resource: "cpu", Used: "9", Hard: "10"},
	}

	hints := GenerateNodeHints(pending, quotas)

	if len(hints) != 2 {
		t.Fatalf("expected 2 hints, got %d", len(hints))
	}

	foundUnschedulable := false
	foundQuota := false
	for _, hint := range hints {
		if strings.Contains(hint, "unschedulable") && strings.Contains(hint, "Cluster Autoscaler") {
			foundUnschedulable = true
		}
		if strings.Contains(hint, "ResourceQuota") && strings.Contains(hint, "compute") {
			foundQuota = true
		}
	}
	if !foundUnschedulable {
		t.Error("expected hint about unschedulable pods")
	}
	if !foundQuota {
		t.Error("expected hint about ResourceQuota")
	}
}
