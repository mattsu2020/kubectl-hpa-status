package kube

import (
	"context"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// --- NewClient tests ---

func TestNewClient_WithInterface(t *testing.T) {
	fakeClient := NewFakeClient()
	client, err := NewClient(Options{}, WithInterface(fakeClient))
	if err != nil {
		t.Fatal(err)
	}
	if client.Interface == nil {
		t.Fatal("expected interface to be set")
	}
	if client.Namespace != "default" {
		t.Fatalf("expected namespace 'default', got %q", client.Namespace)
	}
}

func TestNewClient_WithInterfaceAndExplicitNamespace(t *testing.T) {
	fakeClient := NewFakeClient()
	client, err := NewClient(Options{Namespace: "custom"}, WithInterface(fakeClient))
	if err != nil {
		t.Fatal(err)
	}
	if client.Namespace != "custom" {
		t.Fatalf("expected namespace 'custom', got %q", client.Namespace)
	}
}

func TestNewClient_WithNamespace(t *testing.T) {
	fakeClient := NewFakeClient()
	client, err := NewClient(Options{}, WithInterface(fakeClient), WithNamespace("myns"))
	if err != nil {
		t.Fatal(err)
	}
	if client.Namespace != "myns" {
		t.Fatalf("expected namespace 'myns', got %q", client.Namespace)
	}
}

// --- ListHPAs tests ---

func TestListHPAs_NoChunkSize(t *testing.T) {
	hpa := BuildHPA("default", "web")
	fakeClient := NewFakeClient(hpa)
	client := &Client{Interface: fakeClient, Namespace: "default"}

	result, err := client.ListHPAs(context.Background(), "default", metav1.ListOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 HPA, got %d", len(result.Items))
	}
	if result.Items[0].Name != "web" {
		t.Fatalf("expected HPA 'web', got %q", result.Items[0].Name)
	}
}

func TestListHPAs_WithChunkSize(t *testing.T) {
	hpa1 := BuildHPA("default", "web")
	hpa2 := BuildHPA("default", "api")
	fakeClient := NewFakeClient(hpa1, hpa2)
	client := &Client{Interface: fakeClient, Namespace: "default"}

	result, err := client.ListHPAs(context.Background(), "default", metav1.ListOptions{}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 HPAs, got %d", len(result.Items))
	}
}

// --- Test helper coverage tests ---
// These exercise the test helper functions themselves to improve kube package coverage.

func TestBuildHPA_Defaults(t *testing.T) {
	hpa := BuildHPA("default", "web")
	if hpa.Namespace != "default" {
		t.Fatalf("expected namespace default, got %q", hpa.Namespace)
	}
	if hpa.Name != "web" {
		t.Fatalf("expected name web, got %q", hpa.Name)
	}
	if hpa.Spec.ScaleTargetRef.Kind != "Deployment" {
		t.Fatalf("expected Deployment scaleTargetRef kind")
	}
	if hpa.Spec.MaxReplicas != 10 {
		t.Fatalf("expected maxReplicas 10, got %d", hpa.Spec.MaxReplicas)
	}
	if hpa.Status.CurrentReplicas != 2 {
		t.Fatalf("expected currentReplicas 2, got %d", hpa.Status.CurrentReplicas)
	}
}

func TestBuildHPA_WithOptions(t *testing.T) {
	hpa := BuildHPA("ns", "api",
		WithReplicas(5, 8),
		WithMinMax(3, 20),
		WithResourceMetric("cpu", 70, 85),
		WithConditions(autoscalingv2.HorizontalPodAutoscalerCondition{
			Type:   autoscalingv2.AbleToScale,
			Status: "True",
			Reason: "ReadyForNewScale",
		}),
	)
	if hpa.Status.CurrentReplicas != 5 {
		t.Fatalf("expected current=5, got %d", hpa.Status.CurrentReplicas)
	}
	if hpa.Status.DesiredReplicas != 8 {
		t.Fatalf("expected desired=8, got %d", hpa.Status.DesiredReplicas)
	}
	if *hpa.Spec.MinReplicas != 3 {
		t.Fatalf("expected min=3, got %d", *hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 20 {
		t.Fatalf("expected max=20, got %d", hpa.Spec.MaxReplicas)
	}
	if len(hpa.Spec.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(hpa.Spec.Metrics))
	}
}

func TestBuildEvent(t *testing.T) {
	ev := BuildEvent("default", "web", "ScaledUp", "New size: 5")
	if ev.Namespace != "default" {
		t.Fatalf("expected namespace default, got %q", ev.Namespace)
	}
	if ev.Reason != "ScaledUp" {
		t.Fatalf("expected reason ScaledUp, got %q", ev.Reason)
	}
	if ev.Message != "New size: 5" {
		t.Fatalf("expected message 'New size: 5', got %q", ev.Message)
	}
}

func TestNewFakeClient_Empty(t *testing.T) {
	client := NewFakeClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewFakeClientWithEvents_Empty(t *testing.T) {
	client := NewFakeClientWithEvents(nil, nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// --- extractScaleTargetRef tests ---

func TestExtractScaleTargetRef_Valid(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"scaleTargetRef": map[string]any{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"name":       "my-app",
				},
			},
		},
	}
	ref := extractScaleTargetRef(u)
	if ref == nil {
		t.Fatal("expected non-nil ref")
	}
	if ref.Kind != "Deployment" {
		t.Fatalf("expected Deployment, got %q", ref.Kind)
	}
	if ref.Name != "my-app" {
		t.Fatalf("expected my-app, got %q", ref.Name)
	}
}

func TestExtractScaleTargetRef_NoSpec(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{},
	}
	ref := extractScaleTargetRef(u)
	if ref != nil {
		t.Fatal("expected nil ref for missing spec")
	}
}

func TestExtractScaleTargetRef_NoRef(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{},
		},
	}
	ref := extractScaleTargetRef(u)
	if ref != nil {
		t.Fatal("expected nil ref for missing scaleTargetRef")
	}
}

// --- extractInt32Ptr tests ---

func TestExtractInt32Ptr_Int64(t *testing.T) {
	m := map[string]any{"count": int64(42)}
	result := extractInt32Ptr(m, "count")
	if result == nil || *result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestExtractInt32Ptr_Int(t *testing.T) {
	m := map[string]any{"count": int(42)}
	result := extractInt32Ptr(m, "count")
	if result == nil || *result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestExtractInt32Ptr_Float64(t *testing.T) {
	m := map[string]any{"count": float64(42)}
	result := extractInt32Ptr(m, "count")
	if result == nil || *result != 42 {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestExtractInt32Ptr_Missing(t *testing.T) {
	m := map[string]any{}
	result := extractInt32Ptr(m, "count")
	if result != nil {
		t.Fatalf("expected nil for missing key, got %v", result)
	}
}

func TestExtractInt32Ptr_InvalidType(t *testing.T) {
	m := map[string]any{"count": "not a number"}
	result := extractInt32Ptr(m, "count")
	if result != nil {
		t.Fatalf("expected nil for string type, got %v", result)
	}
}

func TestExtractInt32Ptr_Int64Overflow(t *testing.T) {
	m := map[string]any{"count": int64(1 << 35)}
	result := extractInt32Ptr(m, "count")
	if result != nil {
		t.Fatalf("expected nil for overflow int64, got %v", result)
	}
}

func TestExtractInt32Ptr_IntOverflow(t *testing.T) {
	m := map[string]any{"count": int(1 << 35)}
	result := extractInt32Ptr(m, "count")
	if result != nil {
		t.Fatalf("expected nil for overflow int, got %v", result)
	}
}

func TestExtractInt32Ptr_Float64Overflow(t *testing.T) {
	m := map[string]any{"count": float64(1 << 35)}
	result := extractInt32Ptr(m, "count")
	if result != nil {
		t.Fatalf("expected nil for overflow float64, got %v", result)
	}
}

// --- FetchPodsForScaleTarget tests ---

func TestFetchPodsForScaleTarget_UnsupportedKind(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "CronJob",
				Name: "my-job",
			},
		},
	}
	fakeClient := NewFakeClient()
	_, err := FetchPodsForScaleTarget(context.Background(), fakeClient, "default", hpa)
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

// --- FetchPodDisruptionBudgets tests ---

func TestFetchPodDisruptionBudgets_Empty(t *testing.T) {
	fakeClient := NewFakeClient()
	result := FetchPodDisruptionBudgets(context.Background(), fakeClient, "default", "")
	if result != nil {
		t.Fatalf("expected nil for no PDBs, got %v", result)
	}
}

// --- stringValue tests ---

func TestStringValue(t *testing.T) {
	tests := []struct {
		m    map[string]any
		key  string
		want string
	}{
		{map[string]any{"name": "hello"}, "name", "hello"},
		{map[string]any{"name": 42}, "name", "42"},
		{map[string]any{}, "name", ""},
	}
	for _, tt := range tests {
		got := stringValue(tt.m, tt.key)
		if got != tt.want {
			t.Errorf("stringValue(%v, %q) = %q, want %q", tt.m, tt.key, got, tt.want)
		}
	}
}
