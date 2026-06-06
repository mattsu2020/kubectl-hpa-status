package enrichment

import (
	"context"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// --- Status tests ---

func TestStatus_NilReceiver(t *testing.T) {
	var ec *Context
	s := ec.Status()
	if s.KEDA != nil || s.VPA != nil {
		t.Fatal("expected empty EnrichmentStatus for nil receiver")
	}
}

func TestStatus_NonNilReceiver(t *testing.T) {
	ec := &Context{
		status: EnrichmentStatus{
			KEDA: &EnrichmentEntry{Source: EnrichmentSourceKEDA, State: EnrichmentStateActive},
			VPA:  &EnrichmentEntry{Source: EnrichmentSourceVPA, State: EnrichmentStateDisabled},
		},
	}
	s := ec.Status()
	if s.KEDA.State != EnrichmentStateActive {
		t.Fatalf("expected KEDA active, got %s", s.KEDA.State)
	}
	if s.VPA.State != EnrichmentStateDisabled {
		t.Fatalf("expected VPA disabled, got %s", s.VPA.State)
	}
}

// --- KEDAEnabled / VPAEnabled ---

func TestKEDAEnabled_NilReceiver(t *testing.T) {
	var ec *Context
	if ec.KEDAEnabled() {
		t.Fatal("expected false for nil receiver")
	}
}

func TestVPAEnabled_NilReceiver(t *testing.T) {
	var ec *Context
	if ec.VPAEnabled() {
		t.Fatal("expected false for nil receiver")
	}
}

func TestKEDAEnabled_True(t *testing.T) {
	ec := &Context{kedaEnabled: true}
	if !ec.KEDAEnabled() {
		t.Fatal("expected true")
	}
}

func TestVPAEnabled_True(t *testing.T) {
	ec := &Context{vpaEnabled: true}
	if !ec.VPAEnabled() {
		t.Fatal("expected true")
	}
}

// --- NewContext tests ---

func TestNewContext_BothDisabled(t *testing.T) {
	ec := NewContext(context.Background(), Config{})
	if ec.KEDAEnabled() || ec.VPAEnabled() {
		t.Fatal("expected both disabled")
	}
	s := ec.Status()
	if s.KEDA.State != EnrichmentStateDisabled {
		t.Fatalf("expected KEDA disabled, got %s", s.KEDA.State)
	}
	if s.VPA.State != EnrichmentStateDisabled {
		t.Fatalf("expected VPA disabled, got %s", s.VPA.State)
	}
}

func TestNewContext_KEDARequestedButNoCluster(t *testing.T) {
	// With an invalid kubeconfig, the discovery client should fail.
	ec := NewContext(context.Background(), Config{
		KEDA:       true,
		Kubeconfig: "/nonexistent/kubeconfig",
	})
	if ec.KEDAEnabled() {
		t.Fatal("expected KEDA to not be enabled with invalid kubeconfig")
	}
	s := ec.Status()
	if s.KEDA.State != EnrichmentStateError {
		t.Fatalf("expected KEDA error state, got %s", s.KEDA.State)
	}
}

func TestNewContext_VPARequestedButNoCluster(t *testing.T) {
	ec := NewContext(context.Background(), Config{
		VPA:        true,
		Kubeconfig: "/nonexistent/kubeconfig",
	})
	if ec.VPAEnabled() {
		t.Fatal("expected VPA to not be enabled with invalid kubeconfig")
	}
	s := ec.Status()
	if s.VPA.State != EnrichmentStateError {
		t.Fatalf("expected VPA error state, got %s", s.VPA.State)
	}
}

// --- EnrichmentStatus entry tests ---

func TestEnrichmentStatus_KEDAEntry_Nil(t *testing.T) {
	var s *EnrichmentStatus
	entry := s.KEDAEntry()
	if entry.State != EnrichmentStateDisabled {
		t.Fatalf("expected disabled, got %s", entry.State)
	}
}

func TestEnrichmentStatus_VPAEntry_Nil(t *testing.T) {
	var s *EnrichmentStatus
	entry := s.VPAEntry()
	if entry.State != EnrichmentStateDisabled {
		t.Fatalf("expected disabled, got %s", entry.State)
	}
}

func TestEnrichmentStatus_KEDAEntry_Present(t *testing.T) {
	s := &EnrichmentStatus{
		KEDA: &EnrichmentEntry{Source: EnrichmentSourceKEDA, State: EnrichmentStateActive},
	}
	entry := s.KEDAEntry()
	if entry.State != EnrichmentStateActive {
		t.Fatalf("expected active, got %s", entry.State)
	}
}

func TestEnrichmentStatus_VPAEntry_Present(t *testing.T) {
	s := &EnrichmentStatus{
		VPA: &EnrichmentEntry{Source: EnrichmentSourceVPA, State: EnrichmentStateUnavailable},
	}
	entry := s.VPAEntry()
	if entry.State != EnrichmentStateUnavailable {
		t.Fatalf("expected unavailable, got %s", entry.State)
	}
}

// --- buildKEDAAnalysis tests ---

func TestBuildKEDAAnalysis_BasicInfo(t *testing.T) {
	polling := int32(30)
	cooldown := int32(60)
	info := kube.KEDAInfo{
		ScaledObjectName: "my-scaledobject",
		Triggers: []kube.KEDATrigger{
			{Type: "prometheus", Name: "http-rate", Status: "Active", MetricName: "http_requests_total", Threshold: "100", CurrentValue: "250"},
		},
		PollingInterval: &polling,
		CooldownPeriod:  &cooldown,
		Conditions: []kube.KEDACondition{
			{Type: "Ready", Status: "True", Reason: "ScaledObjectReady", Message: "ScaledObject is ready"},
		},
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "my-app",
			},
			MaxReplicas: 10,
		},
	}

	result := buildKEDAAnalysis(info, hpa)
	if result.ScaledObjectName != "my-scaledobject" {
		t.Fatalf("expected my-scaledobject, got %s", result.ScaledObjectName)
	}
	if len(result.Triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(result.Triggers))
	}
	if result.Triggers[0].Type != "prometheus" {
		t.Fatalf("expected prometheus trigger, got %s", result.Triggers[0].Type)
	}
	if result.PollingInterval == nil || *result.PollingInterval != 30 {
		t.Fatal("expected pollingInterval 30")
	}
	if result.CooldownPeriod == nil || *result.CooldownPeriod != 60 {
		t.Fatal("expected cooldownPeriod 60")
	}
}

func TestBuildKEDAAnalysis_FalseCondition(t *testing.T) {
	info := kube.KEDAInfo{
		ScaledObjectName: "my-so",
		Conditions: []kube.KEDACondition{
			{Type: "Ready", Status: "False", Reason: "Failed", Message: "not ready"},
		},
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "app"},
		},
	}

	result := buildKEDAAnalysis(info, hpa)
	if len(result.Lines) == 0 {
		t.Fatal("expected condition lines for False condition")
	}
	found := false
	for _, line := range result.Lines {
		if containsStr(line, "False") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'False' in condition lines, got %v", result.Lines)
	}
}

func TestBuildKEDAAnalysis_AllConditionsHealthy(t *testing.T) {
	info := kube.KEDAInfo{
		ScaledObjectName: "my-so",
		Conditions: []kube.KEDACondition{
			{Type: "Ready", Status: "True", Reason: "Ready", Message: "ok"},
		},
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "app"},
		},
	}

	result := buildKEDAAnalysis(info, hpa)
	found := false
	for _, line := range result.Lines {
		if containsStr(line, "healthy") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected healthy condition line, got %v", result.Lines)
	}
}

func TestBuildKEDAAnalysis_Fallback(t *testing.T) {
	info := kube.KEDAInfo{
		ScaledObjectName: "my-so",
		Fallback: &kube.KEDAFallback{
			FailureThreshold: 3,
			Replicas:         1,
		},
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "app"},
		},
	}

	result := buildKEDAAnalysis(info, hpa)
	if result.Fallback == nil {
		t.Fatal("expected fallback info")
	}
	if result.Fallback.FailureThreshold != 3 {
		t.Fatalf("expected failureThreshold 3, got %d", result.Fallback.FailureThreshold)
	}
	if result.Fallback.Replicas != 1 {
		t.Fatalf("expected replicas 1, got %d", result.Fallback.Replicas)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- EnrichKEDA tests ---

func TestEnrichKEDA_NotKEDAManaged(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "app"},
		},
	}
	result := EnrichKEDA(context.Background(), nil, hpa)
	if result != nil {
		t.Fatal("expected nil for non-KEDA HPA")
	}
}

// --- EnrichVPA tests ---

func TestEnrichVPA_NilContext(t *testing.T) {
	// EnrichVPA accesses ec.dynClient, so nil context will panic.
	// This test verifies the behavior with a valid but non-enriched context.
	ec := &Context{
		vpaEnabled: false,
		status:     EnrichmentStatus{},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "hpa"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "app"},
		},
	}
	report := &hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Namespace: "default", Name: "hpa"},
	}
	// EnrichVPA doesn't check vpaEnabled; it tries to use dynClient.
	// With nil dynClient, FindConflictingVPA will fail and return early.
	EnrichVPA(context.Background(), ec, hpa, report)
	// No panic is the main assertion.
}

// --- EnrichReport tests ---

func TestEnrichReport_NilContext(t *testing.T) {
	report := &hpaanalysis.StatusReport{}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	EnrichReport(context.Background(), nil, hpa, report, hpaanalysis.HealthWeights{})
	// Should not panic.
}

func TestEnrichReport_BothDisabled(t *testing.T) {
	ec := &Context{kedaEnabled: false, vpaEnabled: false}
	report := &hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Name: "test"},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	EnrichReport(context.Background(), ec, hpa, report, hpaanalysis.HealthWeights{})
	if report.Analysis.KEDAInfo != nil {
		t.Fatal("expected no KEDA info when both disabled")
	}
}

// --- BatchKEDA tests ---

func TestBatchKEDA_NilContext(t *testing.T) {
	hpas := []autoscalingv2.HorizontalPodAutoscaler{{}}
	result := BatchKEDA(context.Background(), nil, hpas)
	if result != nil {
		t.Fatal("expected nil for nil context")
	}
}

func TestBatchKEDA_KEDADisabled(t *testing.T) {
	ec := &Context{kedaEnabled: false}
	hpas := []autoscalingv2.HorizontalPodAutoscaler{{}}
	result := BatchKEDA(context.Background(), ec, hpas)
	if result != nil {
		t.Fatal("expected nil when KEDA disabled")
	}
}

// --- BatchVPA tests ---

func TestBatchVPA_NilContext(t *testing.T) {
	hpas := []autoscalingv2.HorizontalPodAutoscaler{{}}
	result := BatchVPA(context.Background(), nil, hpas)
	if result != nil {
		t.Fatal("expected nil for nil context")
	}
}

func TestBatchVPA_VPADisabled(t *testing.T) {
	ec := &Context{vpaEnabled: false}
	hpas := []autoscalingv2.HorizontalPodAutoscaler{{}}
	result := BatchVPA(context.Background(), ec, hpas)
	if result != nil {
		t.Fatal("expected nil when VPA disabled")
	}
}

// --- scaledObjectMatchesHPA tests ---

func TestScaledObjectMatchesHPA_Match(t *testing.T) {
	so := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"scaleTargetRef": map[string]interface{}{
					"kind": "Deployment",
					"name": "my-app",
				},
			},
		},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "my-app"},
		},
	}
	if !scaledObjectMatchesHPA(so, hpa) {
		t.Fatal("expected match")
	}
}

func TestScaledObjectMatchesHPA_NoMatch(t *testing.T) {
	so := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"scaleTargetRef": map[string]interface{}{
					"kind": "Deployment",
					"name": "other-app",
				},
			},
		},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "my-app"},
		},
	}
	if scaledObjectMatchesHPA(so, hpa) {
		t.Fatal("expected no match")
	}
}

func TestScaledObjectMatchesHPA_EmptyRef(t *testing.T) {
	so := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{},
		},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "my-app"},
		},
	}
	if scaledObjectMatchesHPA(so, hpa) {
		t.Fatal("expected no match with empty ref")
	}
}

func TestScaledObjectMatchesHPA_EmptyKindName(t *testing.T) {
	so := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"scaleTargetRef": map[string]interface{}{
					"kind": "",
					"name": "",
				},
			},
		},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "my-app"},
		},
	}
	if scaledObjectMatchesHPA(so, hpa) {
		t.Fatal("expected no match with empty kind/name")
	}
}

// --- vpaTargetMatchesHPA tests ---

func TestVPATargetMatchesHPA_Match(t *testing.T) {
	vpa := kube.VPAInfo{
		TargetKind: "Deployment",
		TargetName: "my-app",
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "my-app"},
		},
	}
	if !vpaTargetMatchesHPA(vpa, hpa) {
		t.Fatal("expected match")
	}
}

func TestVPATargetMatchesHPA_NoMatch(t *testing.T) {
	vpa := kube.VPAInfo{
		TargetKind: "StatefulSet",
		TargetName: "other-app",
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "my-app"},
		},
	}
	if vpaTargetMatchesHPA(vpa, hpa) {
		t.Fatal("expected no match")
	}
}

// --- hasHPAResourceMetrics tests ---

func TestHasHPAResourceMetrics_True(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{Type: autoscalingv2.ResourceMetricSourceType},
			},
		},
	}
	if !hasHPAResourceMetrics(hpa) {
		t.Fatal("expected true")
	}
}

func TestHasHPAResourceMetrics_False(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{Type: autoscalingv2.ExternalMetricSourceType},
			},
		},
	}
	if hasHPAResourceMetrics(hpa) {
		t.Fatal("expected false")
	}
}

func TestHasHPAResourceMetrics_Empty(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if hasHPAResourceMetrics(hpa) {
		t.Fatal("expected false with no metrics")
	}
}
