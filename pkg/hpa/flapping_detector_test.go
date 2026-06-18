package hpa

import (
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDiagnoseFlapping_TooFewEvents(t *testing.T) {
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: time.Now()},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: time.Now().Add(time.Minute)},
	}
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(events, hpa)
	if result != nil {
		t.Fatalf("expected nil for <3 rescale events, got %+v", result)
	}
}

func TestDiagnoseFlapping_NoFlapping(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(10 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 7", Timestamp: base.Add(20 * time.Minute)},
	}
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Detected {
		t.Error("expected no flapping detection for monotonically increasing replicas")
	}
}

func TestDiagnoseFlapping_HighSeverity(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(1 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base.Add(2 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(3 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base.Add(4 * time.Minute)},
	}
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Detected {
		t.Error("expected flapping to be detected")
	}
	if result.Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL severity, got %s", result.Severity)
	}
	if result.FlipCount < 3 {
		t.Errorf("expected >= 3 flips, got %d", result.FlipCount)
	}
	if result.EventTTLLimitation == "" {
		t.Error("expected EventTTLLimitation to be set")
	}
}

func TestDiagnoseFlapping_MediumSeverity(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(10 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base.Add(20 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(30 * time.Minute)},
	}
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Detected {
		t.Error("expected flapping to be detected")
	}
	if result.Severity != "MEDIUM" && result.Severity != "HIGH" {
		t.Errorf("expected MEDIUM or HIGH severity, got %s", result.Severity)
	}
}

func TestDiagnoseFlapping_ShortStabilizationWindowCause(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(1 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base.Add(2 * time.Minute)},
	}
	window := int32(60)
	hpa := buildFlappingTestHPA(window, nil)
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Detected {
		t.Error("expected flapping detection")
	}

	found := false
	for _, cause := range result.EstimatedCauses {
		if cause.Type == "short-stabilization-window" {
			found = true
			if cause.Confidence != "high" {
				t.Errorf("expected high confidence, got %s", cause.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected short-stabilization-window cause")
	}
}

func TestDiagnoseFlapping_MissingScaleDownPolicyCause(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(1 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base.Add(2 * time.Minute)},
	}
	// No behavior at all → missing policy
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-hpa"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MaxReplicas:    10,
		},
	}
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	found := false
	for _, cause := range result.EstimatedCauses {
		if cause.Type == "missing-scaledown-policy" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-scaledown-policy cause")
	}

	// Should also have a recommendation with a patch
	foundFix := false
	for _, fix := range result.Recommendations {
		if fix.Patch != "" {
			foundFix = true
		}
	}
	if !foundFix {
		t.Error("expected at least one recommendation with a patch")
	}
}

func TestDiagnoseFlapping_TightTargetCause(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base},
		{Reason: "SuccessfulRescale", Message: "New size: 6", Timestamp: base.Add(1 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(2 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 6", Timestamp: base.Add(3 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(4 * time.Minute)},
	}
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	found := false
	for _, cause := range result.EstimatedCauses {
		if cause.Type == "tight-target" {
			found = true
		}
	}
	if !found {
		t.Error("expected tight-target cause for small replica deltas")
	}
}

func TestDiagnoseFlapping_NilHPA(t *testing.T) {
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: time.Now()},
	}
	result := DiagnoseFlapping(events, nil)
	if result != nil {
		t.Fatal("expected nil for nil HPA")
	}
}

func TestDiagnoseFlapping_EmptyEvents(t *testing.T) {
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(nil, hpa)
	if result != nil {
		t.Fatal("expected nil for empty events")
	}
}

func TestDiagnoseFlapping_NonRescaleEventsIgnored(t *testing.T) {
	base := time.Now()
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base},
		{Reason: "FailedGetResourceMetric", Message: "some error", Timestamp: base.Add(time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: base.Add(2 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: base.Add(3 * time.Minute)},
	}
	hpa := buildFlappingTestHPA(300, nil)
	result := DiagnoseFlapping(events, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Detected {
		t.Error("expected flapping detection ignoring non-rescale events")
	}
}

func buildFlappingTestHPA(stabilizationWindow int32, policies []autoscalingv2.HPAScalingPolicy) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := testutil.BuildHPA("default", "test-hpa",
		testutil.WithMinMax(2, 10),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	if policies != nil || stabilizationWindow > 0 {
		hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{
				StabilizationWindowSeconds: &stabilizationWindow,
				Policies:                   policies,
			},
		}
	}
	return hpa
}
