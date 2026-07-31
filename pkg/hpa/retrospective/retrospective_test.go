package retrospective

import (
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	eventutil "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildRetrospectiveTimeline_BasicScaleUp(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5; reason: cpu resource utilization (percentage of request) above target", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 7", Timestamp: now.Add(-10 * time.Minute)},
	}

	since := now.Add(-30 * time.Minute)
	tl := BuildTimeline(events, hpa, since)

	if tl.HPAName != "web" {
		t.Errorf("expected HPAName=web, got %q", tl.HPAName)
	}
	if len(tl.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(tl.Entries))
	}

	// The event exposes the new size but not the predecessor. Current status
	// is a present-time value and must not be projected backwards.
	entry0 := tl.Entries[0]
	if entry0.Category != "rescale" {
		t.Errorf("expected category=rescale, got %q", entry0.Category)
	}
	if !strings.Contains(entry0.Message, "desired <unknown> -> 5") {
		t.Errorf("expected unknown predecessor in message, got %q", entry0.Message)
	}
	if entry0.Source != "event" {
		t.Errorf("expected source=event, got %q", entry0.Source)
	}
	if entry0.Confidence != "low" {
		t.Errorf("expected confidence=low, got %q", entry0.Confidence)
	}
	if entry0.FromReplicas != nil || entry0.ToReplicas == nil || *entry0.ToReplicas != 5 || entry0.ParseValid {
		t.Fatalf("first range must retain only the observed destination: %+v", entry0)
	}

	// Second entry: scale up from 5 to 7 (prevDesired tracked).
	entry1 := tl.Entries[1]
	if !strings.Contains(entry1.Message, "desired 5 -> 7") {
		t.Errorf("expected 'desired 5 -> 7' in message, got %q", entry1.Message)
	}
}

func TestBuildRetrospectiveTimeline_ScaleToZeroIsSuccessfulRescale(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 0; reason: all metrics below target", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 2; reason: external metric above target", Timestamp: now.Add(-10 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-30*time.Minute))
	if len(tl.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %#v", tl.Entries)
	}
	if tl.Entries[0].Confidence != "low" {
		t.Fatalf("scale-to-zero confidence = %q, want low", tl.Entries[0].Confidence)
	}
	if !strings.Contains(tl.Entries[0].Message, "desired <unknown> -> 0") {
		t.Fatalf("unexpected scale-to-zero message: %q", tl.Entries[0].Message)
	}
	if !strings.Contains(tl.Entries[1].Message, "desired 0 -> 2") {
		t.Fatalf("scale-to-zero was not retained as previous desired: %q", tl.Entries[1].Message)
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

	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5; reason: cpu resource utilization above target", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "ScalingLimited", Message: "desired replica count larger than max replica count", Timestamp: now.Add(-19 * time.Minute)},
		{Reason: "FailedGetResourceMetric", Message: "missing request for cpu", Timestamp: now.Add(-10 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-30*time.Minute))
	if len(tl.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %#v", tl.Entries)
	}
	if !strings.Contains(tl.Entries[0].Message, "cpu resource utilization above target") ||
		!strings.Contains(tl.Entries[0].Message, "desired <unknown> -> 5") {
		t.Fatalf("unexpected rescale message: %q", tl.Entries[0].Message)
	}
	if strings.Contains(tl.Entries[0].Message, "92%") || strings.Contains(tl.Entries[0].Message, "60%") {
		t.Fatalf("historical event must not include current metric values: %q", tl.Entries[0].Message)
	}
	if !strings.Contains(tl.Entries[1].Message, "ScalingLimited") ||
		!strings.Contains(tl.Entries[1].Message, "max replica count") {
		t.Fatalf("unexpected scaling limited message: %q", tl.Entries[1].Message)
	}
	if strings.Contains(tl.Entries[1].Message, "maxReplicas=10") {
		t.Fatalf("current maxReplicas must not be projected onto the historical event: %q", tl.Entries[1].Message)
	}
	if !strings.Contains(tl.Entries[2].Message, "FailedGetResourceMetric") ||
		!strings.Contains(tl.Entries[2].Message, "metrics unavailable") {
		t.Fatalf("unexpected metrics message: %q", tl.Entries[2].Message)
	}
}

func TestBuildRetrospectiveTimeline_FailedRescale(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{
		{Reason: "FailedRescale", Message: "missing request for cpu", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-10*time.Minute))

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

func TestBuildTimelineFailedRescaleDoesNotAdvancePreviousDesired(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: now.Add(-3 * time.Minute)},
		{Reason: "FailedRescale", Message: "New size: 9; update conflict", Timestamp: now.Add(-2 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 6", Timestamp: now.Add(-time.Minute)},
	}

	timeline := BuildTimeline(events, hpa, now.Add(-5*time.Minute))
	if len(timeline.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(timeline.Entries))
	}
	if !strings.Contains(timeline.Entries[2].Message, "desired 5 -> 6") {
		t.Fatalf("failed rescale must not advance previous desired: %q", timeline.Entries[2].Message)
	}
}

func TestBuildRetrospectiveTimeline_OtherEventReasons(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{
		{Reason: "DesiredReplicasComputed", Message: "calculated 5", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-10*time.Minute))

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

	tl := BuildTimeline(nil, hpa, now.Add(-30*time.Minute))

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

	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5; reason: cpu resource utilization (percentage of request) above target", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-10*time.Minute))

	if len(tl.Entries) < 1 {
		t.Fatalf("expected at least 1 entry, got %d", len(tl.Entries))
	}
	if !strings.Contains(strings.ToLower(tl.Entries[0].Message), "cpu") {
		t.Errorf("expected raw cpu reason in metric context, got %q", tl.Entries[0].Message)
	}
	if strings.Contains(tl.Entries[0].Message, "142%") {
		t.Errorf("historical event must not include current metric value, got %q", tl.Entries[0].Message)
	}
	if tl.Entries[0].ParseValid || tl.Entries[0].FromReplicas != nil || tl.Entries[0].ToReplicas == nil {
		t.Fatalf("expected destination-only first range, got %+v", tl.Entries[0])
	}
}

func TestBuildTimelineDoesNotUseCurrentReplicaCountAsHistoricalPredecessor(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	hpa.Status.CurrentReplicas = 10
	events := []eventutil.Event{{
		Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: now,
	}}

	timeline := BuildTimeline(events, hpa, now.Add(-time.Minute))
	if len(timeline.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(timeline.Entries))
	}
	entry := timeline.Entries[0]
	if entry.ParseValid || entry.FromReplicas != nil || entry.ToReplicas == nil {
		t.Fatalf("first event must not invent a predecessor: %+v", entry)
	}
	if *entry.ToReplicas != 3 || strings.Contains(entry.Message, "10 -> 3") {
		t.Fatalf("unexpected first range: %+v", entry)
	}
}

func TestBuildTimelineDoesNotInventSuppressionFromCurrentState(t *testing.T) {
	now := time.Now()
	windowSeconds := int32(120)
	hpa := buildRetrospectiveTestHPA("default", "web")
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{
			Policies: []autoscalingv2.HPAScalingPolicy{{
				Type:          autoscalingv2.PodsScalingPolicy,
				Value:         2,
				PeriodSeconds: 60,
			}},
		},
		ScaleDown: &autoscalingv2.HPAScalingRules{
			StabilizationWindowSeconds: &windowSeconds,
		},
	}
	hpa.Status.Conditions = append(hpa.Status.Conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:               autoscalingv2.AbleToScale,
		Status:             corev1.ConditionTrue,
		Reason:             "ScaleDownStabilized",
		LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
	})

	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 4", Timestamp: now.Add(-30 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 6", Timestamp: now.Add(-20 * time.Minute)},
		{Reason: "SuccessfulRescale", Message: "New size: 3", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-40*time.Minute))
	if len(tl.Entries) != len(events) {
		t.Fatalf("current condition/spec must not invent historical entries: %+v", tl.Entries)
	}
	for _, entry := range tl.Entries {
		if entry.Source == "estimated" || entry.Category == "policy-limited" || entry.Category == "stabilized" {
			t.Fatalf("invented historical suppression entry: %+v", entry)
		}
	}
	if !strings.Contains(tl.Entries[2].Message, "desired 6 -> 3") {
		t.Fatalf("observed rescale sequence was not retained: %+v", tl.Entries)
	}
}

func TestBuildTimelineScaleDownStabilizedKeepsCurrentWindowSeparate(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	hpa.Status.Conditions = append(hpa.Status.Conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:               autoscalingv2.AbleToScale,
		Status:             corev1.ConditionTrue,
		Reason:             "ScaleDownStabilized",
		LastTransitionTime: metav1.NewTime(now),
	})
	events := []eventutil.Event{{
		Reason: "ScaleDownStabilized", Message: "recent recommendations were higher",
		Timestamp: now.Add(-20 * time.Minute),
	}}

	tl := BuildTimeline(events, hpa, now.Add(-30*time.Minute))
	if len(tl.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %+v", len(tl.Entries), tl.Entries)
	}
	message := tl.Entries[0].Message
	if !strings.Contains(message, "current effective window=300s") ||
		!strings.Contains(message, "historical duration unavailable") {
		t.Fatalf("current setting and historical uncertainty must be explicit: %q", message)
	}
	if strings.Contains(message, "remaining") || strings.Contains(message, "~") {
		t.Fatalf("current transition time must not be projected onto the past event: %q", message)
	}

	explicit := int32(120)
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &explicit},
	}
	tl = BuildTimeline(events, hpa, now.Add(-30*time.Minute))
	message = tl.Entries[0].Message
	if !strings.Contains(message, "current effective window=120s") ||
		strings.Contains(message, "remaining") || strings.Contains(message, "~") {
		t.Fatalf("explicit current window must not become historical remaining time: %q", message)
	}
}

func TestScaleDownStabilizationWindowSecondsUsesKubernetesDefault(t *testing.T) {
	hpa := buildRetrospectiveTestHPA("default", "web")
	if got := scaleDownStabilizationWindowSeconds(hpa); got != 300 {
		t.Fatalf("nil behavior effective window = %d, want 300", got)
	}
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{},
	}
	if got := scaleDownStabilizationWindowSeconds(hpa); got != 300 {
		t.Fatalf("unspecified scaleDown window = %d, want 300", got)
	}
	explicit := int32(0)
	hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds = &explicit
	if got := scaleDownStabilizationWindowSeconds(hpa); got != 0 {
		t.Fatalf("explicit zero window = %d, want 0", got)
	}
}

func TestBuildTimelineClassifiesTooFewReplicasAsMinimumConstraint(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{{
		Reason: "TooFewReplicas", Message: "the desired replica count is less than the minimum replica count",
		Timestamp: now.Add(-time.Minute),
	}}

	tl := BuildTimeline(events, hpa, now.Add(-5*time.Minute))
	if len(tl.Entries) != 1 || tl.Entries[0].Category != "scaling-limited" {
		t.Fatalf("unexpected entries: %+v", tl.Entries)
	}
	message := tl.Entries[0].Message
	if !strings.Contains(message, "minReplicas") || strings.Contains(message, "maxReplicas") {
		t.Fatalf("TooFewReplicas must describe the minimum constraint: %q", message)
	}
}

func TestBuildRetrospectiveTimeline_Disclaimer(t *testing.T) {
	now := time.Now()
	hpa := buildRetrospectiveTestHPA("default", "web")
	events := []eventutil.Event{
		{Reason: "SuccessfulRescale", Message: "New size: 5", Timestamp: now.Add(-5 * time.Minute)},
	}

	tl := BuildTimeline(events, hpa, now.Add(-30*time.Minute))

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
		ok       bool
	}{
		{"New size: 5; reason: cpu resource utilization above target", 5, true},
		{"New size: 10", 10, true},
		{"New size: 3; reason: All metrics below target", 3, true},
		{"New size: 0; reason: All metrics below target", 0, true},
		{"no size info here", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		result, ok := parseNewSize(tt.message)
		if result != tt.expected || ok != tt.ok {
			t.Errorf("parseNewSize(%q) = (%d, %t), want (%d, %t)", tt.message, result, ok, tt.expected, tt.ok)
		}
	}
}

func TestWriteRetrospectiveTimeline_OutputFormat(t *testing.T) {
	now := time.Now()
	tl := Timeline{
		HPAName:   "web",
		Namespace: "production",
		Since:     now.Add(-30 * time.Minute),
		Until:     now,
		Entries: []Entry{
			{Timestamp: now.Add(-20 * time.Minute), Category: "rescale", Message: "desired 3 -> 5   cpu 142%", Source: "event", Confidence: "high"},
			{Timestamp: now.Add(-10 * time.Minute), Category: "stabilized", Message: "scaleDown suppressed by stabilization window (120s)", Source: "estimated", Confidence: "medium"},
		},
		Disclaimer: "Best-effort reconstruction.",
	}

	var buf strings.Builder
	err := WriteTimeline(&buf, tl, style.NewTheme(false))
	if err != nil {
		t.Fatalf("WriteTimeline returned error: %v", err)
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
	tl := Timeline{
		HPAName:   "web",
		Namespace: "default",
		Since:     now.Add(-30 * time.Minute),
		Until:     now,
		Entries: []Entry{
			{Timestamp: now.Add(-5 * time.Minute), Category: "rescale", Message: "desired 3 -> 5", Source: "event", Confidence: "high"},
		},
		Disclaimer: "Best-effort reconstruction.",
	}

	var buf strings.Builder
	err := WriteMarkdown(&buf, tl)
	if err != nil {
		t.Fatalf("WriteMarkdown returned error: %v", err)
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
	tl := Timeline{
		HPAName:   "web",
		Namespace: "default",
		Since:     now.Add(-30 * time.Minute),
		Until:     now,
		Entries: []Entry{
			{Timestamp: now.Add(-5 * time.Minute), Category: "rescale", Message: "desired 3 -> 5", Source: "event", Confidence: "high"},
		},
		Disclaimer: "Best-effort reconstruction.",
	}

	var buf strings.Builder
	err := WriteHTML(&buf, tl)
	if err != nil {
		t.Fatalf("WriteHTML returned error: %v", err)
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

func TestEntryReplicaRangeRejectsLegacyOverflow(t *testing.T) {
	entry := Entry{Message: "desired 999999999999 -> 2"}
	if from, to, ok := entryReplicaRange(entry); ok {
		t.Fatalf("overflow range accepted as %d -> %d", from, to)
	}
}

// buildRetrospectiveTestHPA creates a minimal HPA for testing purposes.
func buildRetrospectiveTestHPA(namespace, name string) *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA(namespace, name,
		testutil.WithMinMax(1, 10),
		testutil.WithReplicas(3, 3),
		testutil.WithScaleTargetRef("Deployment", name),
		testutil.WithConditions(
			autoscalingv2.HorizontalPodAutoscalerCondition{Type: autoscalingv2.ScalingActive, Status: corev1.ConditionTrue, Reason: "ValidMetricFound"},
		),
	)
}
