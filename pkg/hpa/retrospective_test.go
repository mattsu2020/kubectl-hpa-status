package hpa

import (
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildRetrospectiveTimeline_BasicScaleUp(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5; reason: cpu resource utilization (percentage of request) above target", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 7", Timestamp: now.Add(-10 * time.Minute)},
	}

	since := now.Add(-30 * time.Minute)
	tl := BuildRetrospectiveTimeline(events, hpa, since)

	if tl.HPAName != "web" {
		t.Errorf("expected HPAName=web, got %q", tl.HPAName)
	}
	if len(tl.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(tl.Entries))
	}

	// First entry: scale up from 3 (current) to 5.
	entry0 := tl.Entries[0]
	if entry0.Category != "rescale" {
		t.Errorf("expected category=rescale, got %q", entry0.Category)
	}
	if !strings.Contains(entry0.Message, "desired 3 -> 5") {
		t.Errorf("expected 'desired 3 -> 5' in message, got %q", entry0.Message)
	}
	if entry0.Source != "event" {
		t.Errorf("expected source=event, got %q", entry0.Source)
	}
	if entry0.Confidence != "high" {
		t.Errorf("expected confidence=high, got %q", entry0.Confidence)
	}

	// Second entry: scale up from 5 to 7 (prevDesired tracked).
	entry1 := tl.Entries[1]
	if !strings.Contains(entry1.Message, "desired 5 -> 7") {
		t.Errorf("expected 'desired 5 -> 7' in message, got %q", entry1.Message)
	}
}

func TestBuildRetrospectiveTimeline_DecisionTimelineMessages(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	targetCPU := int32(60)
	currentCPU := int32(92)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &targetCPU,
				},
			},
		},
	}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: corev1.ResourceCPU,
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &currentCPU,
				},
			},
		},
	}

	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5; reason: cpu resource utilization above target", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "ScalingLimited", Message: "desired replica count larger than max replica count", Timestamp: now.Add(-19 * time.Minute)},
		{Reason: "FailedGetResourceMetric", Message: "missing request for cpu", Timestamp: now.Add(-10 * time.Minute)},
	}

	tl := BuildRetrospectiveTimeline(events, hpa, now.Add(-30*time.Minute))
	if len(tl.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %#v", tl.Entries)
	}
	if !strings.Contains(tl.Entries[0].Message, "CPU 92% > target 60%") ||
		!strings.Contains(tl.Entries[0].Message, "desired 3 -> 5") {
		t.Fatalf("unexpected rescale message: %q", tl.Entries[0].Message)
	}
	if !strings.Contains(tl.Entries[1].Message, "ScalingLimited=True") ||
		!strings.Contains(tl.Entries[1].Message, "maxReplicas=10") {
		t.Fatalf("unexpected scaling limited message: %q", tl.Entries[1].Message)
	}
	if !strings.Contains(tl.Entries[2].Message, "FailedGetResourceMetric") ||
		!strings.Contains(tl.Entries[2].Message, "metrics unavailable") {
		t.Fatalf("unexpected metrics message: %q", tl.Entries[2].Message)
	}
}

func TestBuildRetrospectiveTimeline_FailedRescale(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []Event{
		{Reason: "FailedRescale", Message: "missing request for cpu", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildRetrospectiveTimeline(events, hpa, now.Add(-10*time.Minute))

	if len(tl.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(tl.Entries))
	}
	if tl.Entries[0].Category != "rescale" {
		t.Errorf("expected category=rescale, got %q", tl.Entries[0].Category)
	}
	if !strings.Contains(tl.Entries[0].Message, "failed to rescale") {
		t.Errorf("expected 'failed to rescale' in message, got %q", tl.Entries[0].Message)
	}
}

func TestBuildRetrospectiveTimeline_OtherEventReasons(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []Event{
		{Reason: "DesiredReplicasComputed", Message: "calculated 5", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildRetrospectiveTimeline(events, hpa, now.Add(-10*time.Minute))

	if len(tl.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(tl.Entries))
	}
	if tl.Entries[0].Category != "metric-change" {
		t.Errorf("expected category=metric-change, got %q", tl.Entries[0].Category)
	}
	if tl.Entries[0].Confidence != "medium" {
		t.Errorf("expected confidence=medium, got %q", tl.Entries[0].Confidence)
	}
}

func TestBuildRetrospectiveTimeline_EmptyEvents(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")

	tl := BuildRetrospectiveTimeline(nil, hpa, now.Add(-30*time.Minute))

	if len(tl.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(tl.Entries))
	}
	if tl.Disclaimer == "" {
		t.Error("expected disclaimer to be set")
	}
	if len(tl.Warnings) == 0 {
		t.Error("expected warning about no events found")
	}
}

func TestBuildRetrospectiveTimeline_MetricContext(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	cpuUtil := int32(142)
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricStatus{
				Name: corev1.ResourceCPU,
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &cpuUtil,
				},
			},
		},
	}

	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5; reason: cpu resource utilization (percentage of request) above target", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildRetrospectiveTimeline(events, hpa, now.Add(-10*time.Minute))

	if len(tl.Entries) < 1 {
		t.Fatalf("expected at least 1 entry, got %d", len(tl.Entries))
	}
	if !strings.Contains(tl.Entries[0].Message, "CPU") {
		t.Errorf("expected 'CPU' in message for metric context, got %q", tl.Entries[0].Message)
	}
}

func TestBuildRetrospectiveTimeline_Stabilization(t *testing.T) {
	now := time.Now()
	windowSeconds := int32(120)
	hpa := buildRetrospectiveTestHPA("default", "web")
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &windowSeconds,
		},
	}
	hpa.Status.Conditions = append(hpa.Status.Conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:   autoscalingv2.AbleToScale,
		Status: corev1.ConditionTrue,
		Reason: "ScaleDownStabilized",
	})

	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildRetrospectiveTimeline(events, hpa, now.Add(-30*time.Minute))

	// Should have at least the 2 rescale entries plus a possible stabilization entry.
	foundStabilized := false
	for _, entry := range tl.Entries {
		if entry.Category == "stabilized" {
			foundStabilized = true
			if entry.Source != "estimated" {
				t.Errorf("expected source=estimated for stabilized entry, got %q", entry.Source)
			}
		}
	}
	if !foundStabilized {
		t.Error("expected a stabilized entry to be inserted between scale events")
	}
}

func TestBuildRetrospectiveTimeline_Disclaimer(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildRetrospectiveTimeline(events, hpa, now.Add(-30*time.Minute))

	if tl.Disclaimer == "" {
		t.Error("expected disclaimer to be set")
	}
	if !strings.Contains(tl.Disclaimer, "Best-effort") {
		t.Errorf("expected disclaimer to contain 'Best-effort', got %q", tl.Disclaimer)
	}
}

func TestParseNewSize(t *testing.T) {
	tests := []struct {
		message  string
		expected int32
	}{
		{"New size: 5; reason: cpu resource utilization above target", 5},
		{"New size: 10", 10},
		{"New size: 3; reason: All metrics below target", 3},
		{"no size info here", 0},
		{"", 0},
	}

	for _, tt := range tests {
		result := parseNewSize(tt.message)
		if result != tt.expected {
			t.Errorf("parseNewSize(%q) = %d, want %d", tt.message, result, tt.expected)
		}
	}
}

func TestWriteRetrospectiveTimeline_OutputFormat(t *testing.T) {
	now := time.Now()
	tl := RetrospectiveTimeline{
		HPAName:   "web",
		Namespace: "production",
		Since:     now.Add(-30 * time.Minute),
		Until:     now,
		Entries: []RetrospectiveEntry{
			{Timestamp: now.Add(-20 * time.Minute), Category: "rescale", Message: "desired 3 -> 5   cpu 142%", Source: "event", Confidence: "high"},
			{Timestamp: now.Add(-10 * time.Minute), Category: "stabilized", Message: "scaleDown suppressed by stabilization window (120s)", Source: "estimated", Confidence: "medium"},
		},
		Disclaimer: "Best-effort reconstruction.",
	}

	var buf strings.Builder
	err := WriteRetrospectiveTimeline(&buf, tl, style.NewTheme(false))
	if err != nil {
		t.Fatalf("WriteRetrospectiveTimeline returned error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "HPA Scaling Timeline: web (production)") {
		t.Errorf("expected header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "desired 3 -> 5") {
		t.Errorf("expected scale-up entry in output, got:\n%s", output)
	}
	if !strings.Contains(output, "stabilization window") {
		t.Errorf("expected stabilization entry in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Note: Best-effort") {
		t.Errorf("expected disclaimer in output, got:\n%s", output)
	}
}

func TestWriteRetrospectiveTimeline_Markdown(t *testing.T) {
	now := time.Now()
	tl := RetrospectiveTimeline{
		HPAName:   "web",
		Namespace: "default",
		Since:     now.Add(-30 * time.Minute),
		Until:     now,
		Entries: []RetrospectiveEntry{
			{Timestamp: now.Add(-5 * time.Minute), Category: "rescale", Message: "desired 3 -> 5", Source: "event", Confidence: "high"},
		},
		Disclaimer: "Best-effort reconstruction.",
	}

	var buf strings.Builder
	err := WriteRetrospectiveMarkdown(&buf, tl)
	if err != nil {
		t.Fatalf("WriteRetrospectiveMarkdown returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "# HPA Scaling Timeline") {
		t.Errorf("expected markdown header, got:\n%s", output)
	}
	if !strings.Contains(output, "| Time |") {
		t.Errorf("expected markdown table header, got:\n%s", output)
	}
}

func TestWriteRetrospectiveTimeline_HTML(t *testing.T) {
	now := time.Now()
	tl := RetrospectiveTimeline{
		HPAName:   "web",
		Namespace: "default",
		Since:     now.Add(-30 * time.Minute),
		Until:     now,
		Entries: []RetrospectiveEntry{
			{Timestamp: now.Add(-5 * time.Minute), Category: "rescale", Message: "desired 3 -> 5", Source: "event", Confidence: "high"},
		},
		Disclaimer: "Best-effort reconstruction.",
	}

	var buf strings.Builder
	err := WriteRetrospectiveHTML(&buf, tl)
	if err != nil {
		t.Fatalf("WriteRetrospectiveHTML returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<!DOCTYPE html>") {
		t.Errorf("expected HTML document, got:\n%s", output)
	}
	if !strings.Contains(output, "<table>") {
		t.Errorf("expected HTML table, got:\n%s", output)
	}
	if !strings.Contains(output, "desired 3 -&gt; 5") && !strings.Contains(output, "desired 3 -> 5") {
		t.Errorf("expected scale-up entry in HTML, got:\n%s", output)
	}
}

// buildRetrospectiveTestHPA creates a minimal HPA for testing purposes.
func buildRetrospectiveTestHPA(namespace, name string) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: name},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 3,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
			},
		},
	}
}
