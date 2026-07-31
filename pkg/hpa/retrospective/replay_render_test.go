package retrospective

import (
	"bytes"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// replayFixtureTimeline builds a timeline that walks AnalyzeReplay through all
// entry categories: rescales (up and down), a metrics outage, a maxReplicas
// cap, a stabilization window, and a CPU metric change near tolerance.
func replayFixtureTimeline(base time.Time) Timeline {
	return Timeline{
		HPAName:   "web",
		Namespace: "default",
		Since:     base,
		Until:     base.Add(30 * time.Minute),
		Entries: []Entry{
			{Timestamp: base, Category: "rescale", Message: "desired 2 -> 4; cpu resource utilization above target", Source: "event"},
			{Timestamp: base.Add(2 * time.Minute), Category: "metrics-unavailable", Message: "unable to get metrics for resource cpu", Source: "event"},
			{Timestamp: base.Add(5 * time.Minute), Category: "scaling-limited", Message: "ScalingLimited: True", Source: "status"},
			{Timestamp: base.Add(8 * time.Minute), Category: "rescale", Message: "desired 4 -> 10; cpu resource utilization above target", Source: "event"},
			{Timestamp: base.Add(12 * time.Minute), Category: "stabilized", Message: "ScaleDownStabilized: recent recommendations were higher", Source: "status"},
			{Timestamp: base.Add(16 * time.Minute), Category: "rescale", Message: "desired 10 -> 6; memory resource utilization below target", Source: "event"},
			{Timestamp: base.Add(20 * time.Minute), Category: "metric-change", Message: "cpu utilization changed to 52%", Source: "metrics"},
		},
	}
}

func replayFixtureHPA() *autoscalingv2.HorizontalPodAutoscaler {
	window := int32(300)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment", Name: "web",
			},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptrInt32ForReplay(50),
					},
				},
			}},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
	}
}

func ptrInt32ForReplay(v int32) *int32 { return &v }

func TestAnalyzeReplay(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	analysis := AnalyzeReplay(replayFixtureTimeline(base), replayFixtureHPA())

	if len(analysis.ControlCycles) == 0 {
		t.Error("expected control cycles from rescale entries")
	}
	if len(analysis.Bottlenecks) == 0 {
		t.Error("expected bottlenecks from metrics-unavailable/scaling-limited entries")
	}
	if analysis.Summary == "" {
		t.Error("expected a non-empty summary")
	}

	var sawScaleUp, sawScaleDown bool
	for _, c := range analysis.ControlCycles {
		switch c.Decision {
		case "scale-up":
			sawScaleUp = true
		case "scale-down":
			sawScaleDown = true
		}
	}
	if !sawScaleUp || !sawScaleDown {
		t.Errorf("expected both scale directions in control cycles: %+v", analysis.ControlCycles)
	}
}

func TestAnalyzeReplayEmptyTimeline(t *testing.T) {
	analysis := AnalyzeReplay(Timeline{}, replayFixtureHPA())
	if !strings.Contains(analysis.Summary, "No timeline entries") {
		t.Fatalf("empty timeline should say so, got %q", analysis.Summary)
	}
}

func TestAnalyzeReplayUsesTypedReplicaRangeAndRejectsInvalidRescale(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	from, to := int32(0), int32(3)
	timeline := Timeline{
		Since: base,
		Entries: []Entry{
			{
				Timestamp: base.Add(time.Minute), Category: "rescale",
				Message:      "localized message without replica numbers",
				FromReplicas: &from, ToReplicas: &to, ParseValid: true,
			},
			{
				Timestamp: base.Add(2 * time.Minute), Category: "rescale",
				Message: "failed to parse legacy event",
			},
		},
	}

	analysis := AnalyzeReplay(timeline, replayFixtureHPA())
	if len(analysis.ControlCycles) != 1 {
		t.Fatalf("control cycles = %d, want only typed rescale", len(analysis.ControlCycles))
	}
	if analysis.ControlCycles[0].Decision != "scale-up" ||
		analysis.ControlCycles[0].InputReplicas != 0 ||
		analysis.ControlCycles[0].OutputReplicas != 3 {
		t.Fatalf("typed control cycle = %+v", analysis.ControlCycles[0])
	}
	if len(analysis.Bottlenecks) != 1 || analysis.Bottlenecks[0].Type != "rescale" {
		t.Fatalf("invalid rescale bottleneck missing: %+v", analysis.Bottlenecks)
	}
}

func TestAnalyzeReplayTreatsDestinationOnlyFirstRescaleAsBoundary(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	firstTo, secondFrom, secondTo := int32(4), int32(4), int32(7)
	timeline := Timeline{
		Since: base,
		Entries: []Entry{
			{
				Timestamp: base.Add(time.Minute), Category: "rescale",
				Message: "desired <unknown> -> 4", ToReplicas: &firstTo,
			},
			{
				Timestamp: base.Add(2 * time.Minute), Category: "rescale",
				Message: "desired 4 -> 7", FromReplicas: &secondFrom,
				ToReplicas: &secondTo, ParseValid: true,
			},
		},
	}

	analysis := AnalyzeReplay(timeline, replayFixtureHPA())
	if len(analysis.Bottlenecks) != 0 {
		t.Fatalf("destination-only boundary must not be a bottleneck: %+v", analysis.Bottlenecks)
	}
	if len(analysis.ControlCycles) != 1 {
		t.Fatalf("control cycles = %d, want 1", len(analysis.ControlCycles))
	}
	cycle := analysis.ControlCycles[0]
	if !cycle.Start.Equal(timeline.Entries[0].Timestamp) || cycle.InputReplicas != 4 || cycle.OutputReplicas != 7 {
		t.Fatalf("unexpected control cycle after boundary: %+v", cycle)
	}
}

func TestAnalyzeReplayDoesNotInferPastToleranceFromCurrentStatus(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	hpa := replayFixtureHPA()
	current := int32(52)
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{
			Name: corev1.ResourceCPU,
			Current: autoscalingv2.MetricValueStatus{
				AverageUtilization: &current,
			},
		},
	}}
	timeline := Timeline{
		Since: base,
		Entries: []Entry{{
			Timestamp: base.Add(time.Minute), Category: "metric-change",
			Message: "cpu utilization changed", Source: "event",
		}},
	}

	analysis := AnalyzeReplay(timeline, hpa)
	if len(analysis.ReplayToleranceEffects) != 0 {
		t.Fatalf("current snapshot must not be used as past metric evidence: %+v", analysis.ReplayToleranceEffects)
	}
}

func TestAnalyzeReplayCreatesOneUnknownStabilizationWindow(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	timeline := replayFixtureTimeline(base)
	analysis := AnalyzeReplay(timeline, replayFixtureHPA())

	if len(analysis.StabilizationWindows) != 1 {
		t.Fatalf("stabilization windows = %d, want 1: %+v",
			len(analysis.StabilizationWindows), analysis.StabilizationWindows)
	}
	if analysis.StabilizationWindows[0].SuppressedScaleDown != nil {
		t.Fatalf("successful rescale does not reveal suppressed replica count: %+v",
			analysis.StabilizationWindows[0])
	}
}

func TestAnalyzeReplayDoesNotTreatReachingMaxAsCapEvidence(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	from, to := int32(4), int32(10)
	timeline := Timeline{
		Since: base,
		Until: base.Add(10 * time.Minute),
		Entries: []Entry{{
			Timestamp: base.Add(time.Minute), Category: "rescale",
			Message: "desired 4 -> 10", FromReplicas: &from, ToReplicas: &to, ParseValid: true,
		}},
	}

	analysis := AnalyzeReplay(timeline, replayFixtureHPA())
	if len(analysis.ControlCycles) != 1 || analysis.ControlCycles[0].Decision != "scale-up" {
		t.Fatalf("reaching max without cap evidence must remain scale-up: %+v", analysis.ControlCycles)
	}
	if len(analysis.Bottlenecks) != 0 {
		t.Fatalf("reaching max alone must not create a bottleneck: %+v", analysis.Bottlenecks)
	}
}

func TestAnalyzeReplayMarksCapOnlyWithRecordedEvidence(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	from, to := int32(4), int32(10)
	timeline := Timeline{
		Since: base,
		Until: base.Add(10 * time.Minute),
		Entries: []Entry{{
			Timestamp: base.Add(time.Minute), Category: "rescale",
			Message:      "desired 4 -> 10; requested replicas=12, maxReplicas=10",
			FromReplicas: &from, ToReplicas: &to, ParseValid: true,
		}},
	}

	analysis := AnalyzeReplay(timeline, replayFixtureHPA())
	if len(analysis.ControlCycles) != 1 || analysis.ControlCycles[0].Decision != "capped" {
		t.Fatalf("recorded cap evidence must classify the cycle as capped: %+v", analysis.ControlCycles)
	}
	if len(analysis.Bottlenecks) != 1 || !strings.Contains(analysis.Bottlenecks[0].Message, "recorded") {
		t.Fatalf("recorded cap evidence must create a policy bottleneck: %+v", analysis.Bottlenecks)
	}
}

func TestAnalyzeReplayPreservesMinimumConstraintEvidence(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	timeline := Timeline{
		Since: base,
		Until: base.Add(10 * time.Minute),
		Entries: []Entry{{
			Timestamp: base.Add(time.Minute), Category: "scaling-limited",
			Message: "TooFewReplicas      constrained by minReplicas", Source: "event",
		}},
	}

	analysis := AnalyzeReplay(timeline, replayFixtureHPA())
	if len(analysis.Bottlenecks) != 1 {
		t.Fatalf("bottlenecks = %d, want 1: %+v", len(analysis.Bottlenecks), analysis.Bottlenecks)
	}
	message := analysis.Bottlenecks[0].Message
	if !strings.Contains(message, "minReplicas") || strings.Contains(message, "maxReplicas") {
		t.Fatalf("minimum constraint evidence was rewritten incorrectly: %q", message)
	}
}

func TestAnalyzeReplayObservesFinalStabilizationUntilTimelineEnd(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	timeline := Timeline{
		Since: base,
		Until: base.Add(10 * time.Minute),
		Entries: []Entry{{
			Timestamp: base.Add(4 * time.Minute), Category: "stabilized",
			Message: "ScaleDownStabilized", Source: "event",
		}},
	}

	analysis := AnalyzeReplay(timeline, replayFixtureHPA())
	if len(analysis.StabilizationWindows) != 1 {
		t.Fatalf("stabilization windows = %d, want 1", len(analysis.StabilizationWindows))
	}
	window := analysis.StabilizationWindows[0]
	if !window.End.Equal(timeline.Until) || window.Duration != 6*time.Minute {
		t.Fatalf("final stabilization observation = %+v, want end=%s duration=6m", window, timeline.Until)
	}
}

func TestWriteReplayRenderers(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	tl := replayFixtureTimeline(base)
	analysis := AnalyzeReplay(tl, replayFixtureHPA())

	t.Run("text", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteReplayText(&buf, analysis, tl, style.Theme{}); err != nil {
			t.Fatalf("WriteReplayText: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"web", analysis.Summary} {
			if !strings.Contains(out, want) {
				t.Errorf("replay text missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("markdown", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteReplayMarkdown(&buf, analysis, tl); err != nil {
			t.Fatalf("WriteReplayMarkdown: %v", err)
		}
		if buf.Len() == 0 {
			t.Fatal("markdown output is empty")
		}
	})

	t.Run("html", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteReplayHTML(&buf, analysis, tl); err != nil {
			t.Fatalf("WriteReplayHTML: %v", err)
		}
		if !strings.Contains(buf.String(), "<") {
			t.Fatal("HTML output contains no markup")
		}
	})
}
