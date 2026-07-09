package hpa

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestStructuredInterpretation_ScalingInactive(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}

	got := Analyze(hpa, true)
	if len(got.StructuredInterpretation) == 0 {
		t.Fatalf("expected structured interpretation, got none")
	}
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "ScalingInactive" {
			found = true
			if msg.Severity != "error" {
				t.Fatalf("expected severity=error, got %s", msg.Severity)
			}
			if msg.NextStep == "" {
				t.Fatalf("expected non-empty NextStep for ScalingInactive")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=ScalingInactive, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredInterpretation_StaleStatus(t *testing.T) {
	hpa := baseHPA()
	observed := int64(1)
	hpa.Generation = 3
	hpa.Status.ObservedGeneration = &observed

	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "StaleStatus" {
			found = true
			if msg.Severity != "warning" {
				t.Fatalf("expected severity=warning, got %s", msg.Severity)
			}
			if msg.NextStep == "" {
				t.Fatalf("expected non-empty NextStep for StaleStatus")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=StaleStatus, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredInterpretation_ScaleDownStabilized(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 8
	hpa.Status.DesiredReplicas = 8
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ScaleDownStabilized", Message: "recent recommendations were higher"},
	}

	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "ScaleDownStabilized" {
			found = true
			if msg.Severity != "info" {
				t.Fatalf("expected severity=info, got %s", msg.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=ScaleDownStabilized, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredActions_RestoreMetrics(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}

	got := Analyze(hpa, true)
	if len(got.StructuredActions) == 0 {
		t.Fatalf("expected structured actions, got none")
	}
	found := false
	for _, msg := range got.StructuredActions {
		if msg.Reason == "RestoreMetrics" {
			found = true
			if msg.Severity != "error" {
				t.Fatalf("expected severity=error, got %s", msg.Severity)
			}
			if msg.NextStep == "" {
				t.Fatalf("expected non-empty NextStep for RestoreMetrics")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=RestoreMetrics, got %#v", got.StructuredActions)
	}
}

// TestActionsSSOT ensures RecommendedActions and buildStructuredActions are
// derived from the same collectActionCases list (same count and order). Human
// text and structured Message may differ in wording, but every string action
// must have a paired structured entry.

func TestActionsSSOT(t *testing.T) {
	cases := []struct {
		name string
		hpa  *autoscalingv2.HorizontalPodAutoscaler
	}{
		{
			name: "scaling inactive with external metric",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := baseHPA()
				h.Status.CurrentReplicas = 3
				h.Status.DesiredReplicas = 0
				h.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetExternalMetric"},
				}
				h.Spec.Metrics = []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
						},
					},
				}
				return h
			}(),
		},
		{
			name: "steady no-op",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := baseHPA()
				h.Status.CurrentReplicas = 3
				h.Status.DesiredReplicas = 3
				h.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "AbleToScale", Status: corev1.ConditionTrue, Reason: "ReadyForNewScale"},
				}
				return h
			}(),
		},
		{
			name: "capped at maxReplicas",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				h := baseHPA()
				h.Spec.MaxReplicas = 5
				h.Status.CurrentReplicas = 5
				h.Status.DesiredReplicas = 5
				h.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
					{Type: "ScalingActive", Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
					{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				}
				return h
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actions := RecommendedActions(tc.hpa, 1)
			structured := buildStructuredActions(tc.hpa, 1)
			if len(actions) != len(structured) {
				t.Fatalf("SSOT drift: len(Actions)=%d len(StructuredActions)=%d\nactions=%v\nstructured=%#v",
					len(actions), len(structured), actions, structured)
			}
			if len(actions) == 0 {
				t.Fatal("expected at least one action for this fixture")
			}
			for i := range actions {
				if structured[i].Reason == "" {
					t.Fatalf("structured[%d] missing Reason", i)
				}
				if structured[i].Message == "" {
					t.Fatalf("structured[%d] missing Message", i)
				}
				if actions[i] == "" {
					t.Fatalf("actions[%d] empty", i)
				}
			}
		})
	}
}

func TestInterpretDetectsMetricDisagreement(t *testing.T) {
	target := resource.MustParse("10")
	lowCurrent := resource.MustParse("5")
	hpa := baseHPA()
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		resourceMetricSpec(corev1.ResourceCPU, 80),
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		resourceMetricStatus(corev1.ResourceCPU, 50), // ratio ~0.625, below target
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{
				Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Current: autoscalingv2.MetricValueStatus{Value: &lowCurrent}, // ratio 0.5, below target
			},
		},
	}

	// No disagreement when both are below target
	got := Analyze(hpa, true)
	if containsLine(got.Interpretation, "Metric disagreement detected") {
		t.Fatal("did not expect disagreement when both metrics are below target")
	}

	// Now make the external metric above target to create disagreement
	highCurrent := resource.MustParse("20")
	hpa.Status.CurrentMetrics[1] = autoscalingv2.MetricStatus{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric:  autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			Current: autoscalingv2.MetricValueStatus{Value: &highCurrent}, // ratio 2.0, above target
		},
	}

	got2 := Analyze(hpa, true)
	if !containsLine(got2.Interpretation, "Metric disagreement detected") {
		t.Fatalf("expected metric disagreement warning, got %#v", got2.Interpretation)
	}
	if !containsLine(got2.Interpretation, "cpu") {
		t.Fatalf("expected cpu mentioned in disagreement, got %#v", got2.Interpretation)
	}
	if !containsLine(got2.Interpretation, "queue_depth") {
		t.Fatalf("expected queue_depth mentioned in disagreement, got %#v", got2.Interpretation)
	}
}

func TestStructuredInterpretation_IncludesExternalMetricDiagnostics(t *testing.T) {
	hpa := baseHPA()
	target := resource.MustParse("10")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "ExternalMetricDiagnostic" && strings.Contains(msg.Message, "queue_depth") {
			found = true
			if msg.Severity == "" {
				t.Fatalf("expected non-empty severity for ExternalMetricDiagnostic")
			}
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=ExternalMetricDiagnostic, got %#v", got.StructuredInterpretation)
	}
}

func TestStructuredInterpretation_IncludesLimitation(t *testing.T) {
	hpa := baseHPA()
	got := Analyze(hpa, true)
	found := false
	for _, msg := range got.StructuredInterpretation {
		if msg.Reason == "Limitation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected StructuredMessage with reason=Limitation, got %#v", got.StructuredInterpretation)
	}
}

func TestInterpret_NoDuplicateLimitation(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 3
	hpa.Status.DesiredReplicas = 0
	hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: "ScalingActive", Status: corev1.ConditionFalse, Reason: "FailedGetResourceMetric", Message: "missing cpu metrics"},
	}
	got := Analyze(hpa, true)
	count := 0
	for _, line := range got.Interpretation {
		if strings.Contains(line, "This plugin uses existing HPA status") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 limitation line, got %d", count)
	}
}

func TestCollectDiagnosticsIncludesAllPhases(t *testing.T) {
	hpa := baseHPA()
	hpa.Name = "keda-hpa-worker"
	hpa.Labels = map[string]string{"scaledobject.keda.sh/name": "worker"}
	target := resource.MustParse("10")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	entries := CollectDiagnostics(hpa, 2)
	reasons := make(map[string]int)
	for _, e := range entries {
		reasons[e.Reason]++
	}

	// Should have core cases (NoScaleVisible, Limitation)
	if reasons["NoScaleVisible"] == 0 {
		t.Fatal("expected NoScaleVisible from core cases")
	}
	// Should have ExternalMetricDiagnostic
	if reasons["ExternalMetricDiagnostic"] == 0 {
		t.Fatal("expected ExternalMetricDiagnostic")
	}
	// Should have KEDADiagnostic
	if reasons["KEDADiagnostic"] == 0 {
		t.Fatal("expected KEDADiagnostic")
	}
	// Should have Limitation
	if reasons["Limitation"] == 0 {
		t.Fatal("expected Limitation")
	}
}

func TestCollectDiagnosticsTextStructuredParity(t *testing.T) {
	hpa := baseHPA()
	target := resource.MustParse("10")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
				Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType, Value: &target},
			},
		},
	}

	text := Interpret(hpa, 2)
	structured := buildStructuredInterpretation(hpa, 2)

	if len(text) != len(structured) {
		t.Fatalf("text (%d) and structured (%d) should have same length", len(text), len(structured))
	}
	for i, msg := range structured {
		if text[i] != msg.Message {
			t.Fatalf("entry %d: text=%q != structured.Message=%q", i, text[i], msg.Message)
		}
	}
}
