package cmd

import (
	"reflect"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestFormatReplayDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-5 * time.Second, "0s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{time.Hour, "1h0m"},
		{90 * time.Minute, "1h30m"},
		{2*time.Hour + 15*time.Minute, "2h15m"},
	}
	for _, tc := range tests {
		if got := formatReplayDuration(tc.d); got != tc.want {
			t.Fatalf("formatReplayDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestReplayFlappingScore(t *testing.T) {
	tests := []struct {
		scaleEvents, directionFlips int
		want                        string
	}{
		{0, 0, "none"},
		{0, 1, "low"},
		{4, 0, "low"},
		{8, 0, "medium"},
		{0, 3, "medium"},
		{15, 0, "high"},
		{0, 6, "high"},
		{20, 10, "high"},
	}
	for _, tc := range tests {
		if got := replayFlappingScore(tc.scaleEvents, tc.directionFlips); got != tc.want {
			t.Fatalf("replayFlappingScore(scale=%d, flips=%d) = %q, want %q",
				tc.scaleEvents, tc.directionFlips, got, tc.want)
		}
	}
}

func TestParseReplayScore(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantNil bool
	}{
		{in: "", wantNil: true},
		{in: "single", want: []string{"single"}},
		{in: "a,b,c", want: []string{"a", "b", "c"}},
		{in: "a, b , c", want: []string{"a", "b", "c"}}, // trims spaces
		{in: "a,,b", want: []string{"a", "b"}},           // drops empties
		// All-empty parts yield a non-nil empty slice (make([]string, 0, n)).
		{in: "  ,  ,  ", want: []string{}},
	}
	for _, tc := range tests {
		got := parseReplayScore(tc.in)
		if tc.wantNil {
			if got != nil {
				t.Fatalf("parseReplayScore(%q) = %v, want nil", tc.in, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("parseReplayScore(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestReplaySnapshotDurationSeconds(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(60 * time.Second)
	t2 := t0.Add(120 * time.Second)
	trace := hpaanalysis.TimelineTrace{
		Interval: 5 * time.Minute,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: t0},
			{Timestamp: t1},
			{Timestamp: t2},
		},
	}
	tests := []struct {
		index int
		want  int64
	}{
		{0, 60}, // t1 - t0 = 60s
		{1, 60}, // t2 - t1 = 60s
		{2, 300}, // no next snapshot → falls back to trace.Interval (5m=300s)
	}
	for _, tc := range tests {
		if got := replaySnapshotDurationSeconds(trace, tc.index); got != tc.want {
			t.Fatalf("replaySnapshotDurationSeconds(index=%d) = %d, want %d", tc.index, got, tc.want)
		}
	}

	// Trace with no snapshots and no interval returns 0.
	emptyTrace := hpaanalysis.TimelineTrace{}
	if got := replaySnapshotDurationSeconds(emptyTrace, 0); got != 0 {
		t.Fatalf("empty trace duration = %d, want 0", got)
	}
}

func TestSnapshotCapped(t *testing.T) {
	snap := hpaanalysis.TimelineSnapshot{Desired: 10}
	tests := []struct {
		name         string
		snap         hpaanalysis.TimelineSnapshot
		maxReplicas  int32
		demandTrace  *hpaanalysis.TimelineTrace
		want         bool
	}{
		{name: "maxReplicas zero disables capping", snap: snap, maxReplicas: 0, want: false},
		{name: "desired reaches max", snap: hpaanalysis.TimelineSnapshot{Desired: 10}, maxReplicas: 10, want: true},
		{name: "desired below max", snap: hpaanalysis.TimelineSnapshot{Desired: 5}, maxReplicas: 10, want: false},
		{name: "demand overrides when present", snap: hpaanalysis.TimelineSnapshot{Desired: 3}, maxReplicas: 10,
			demandTrace: &hpaanalysis.TimelineTrace{Snapshots: []hpaanalysis.TimelineSnapshot{{Desired: 15}}}, want: true},
		{name: "demand present but under max", snap: hpaanalysis.TimelineSnapshot{Desired: 3}, maxReplicas: 10,
			demandTrace: &hpaanalysis.TimelineTrace{Snapshots: []hpaanalysis.TimelineSnapshot{{Desired: 5}}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotCapped(tc.snap, 0, tc.maxReplicas, tc.demandTrace); got != tc.want {
				t.Fatalf("snapshotCapped = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasTimelineCondition(t *testing.T) {
	snap := hpaanalysis.TimelineSnapshot{
		Conditions: []hpaanalysis.Condition{
			{Type: "ScalingLimited", Status: "True"},
			{Type: "ScalingActive", Status: "False"},
		},
	}
	if !hasTimelineCondition(snap, "ScalingLimited", "True") {
		t.Fatalf("expected ScalingLimited=True to match")
	}
	if hasTimelineCondition(snap, "ScalingLimited", "False") {
		t.Fatalf("ScalingLimited=False should not match")
	}
	if hasTimelineCondition(snap, "AbleToScale", "True") {
		t.Fatalf("missing condition should not match")
	}
}

func TestSummarizeReplayTraceWithDemand(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trace := hpaanalysis.TimelineTrace{
		Interval: 60 * time.Second,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: t0, Desired: 5, Health: "OK"},
			{Timestamp: t0.Add(60 * time.Second), Desired: 10, Health: "LIMITED"},
			{Timestamp: t0.Add(120 * time.Second), Desired: 7, Health: "OK"},
			{Timestamp: t0.Add(180 * time.Second), Desired: 10, Health: "LIMITED"},
		},
	}
	summary := summarizeReplayTraceWithDemand(trace, 10, nil)

	if summary.Snapshots != 4 {
		t.Fatalf("Snapshots = %d, want 4", summary.Snapshots)
	}
	if summary.PeakReplicas != 10 {
		t.Fatalf("PeakReplicas = %d, want 10", summary.PeakReplicas)
	}
	// Desired changes: 5→10 (event 1), 10→7 (event 2), 7→10 (event 3) = 3 scale events.
	if summary.ScaleEvents != 3 {
		t.Fatalf("ScaleEvents = %d, want 3", summary.ScaleEvents)
	}
	// Direction: 5→10 (up, lastDirection=0 so no flip), 10→7 (down, flip 1),
	// 7→10 (up, flip 2). Two direction reversals total.
	if summary.DirectionFlips != 2 {
		t.Fatalf("DirectionFlips = %d, want 2", summary.DirectionFlips)
	}
	if summary.MaxReplicasReached != 2 {
		t.Fatalf("MaxReplicasReached = %d, want 2", summary.MaxReplicasReached)
	}
	if summary.EstimatedUnderProvision != 2 {
		t.Fatalf("EstimatedUnderProvision = %d, want 2 (LIMITED health snapshots)", summary.EstimatedUnderProvision)
	}
	if summary.FlappingScore != "low" {
		t.Fatalf("FlappingScore = %q, want %q (3 events, 1 flip → low)", summary.FlappingScore, "low")
	}
	if summary.FlappingLabel != summary.FlappingScore {
		t.Fatalf("FlappingLabel = %q, want %q", summary.FlappingLabel, summary.FlappingScore)
	}
	if summary.CappedDuration != "2m" {
		t.Fatalf("CappedDuration = %q, want 2m (2 capped snapshots × 60s)", summary.CappedDuration)
	}
}

func TestApplyReplayCandidateClampsAndDoesNotMutate(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	original := hpaanalysis.TimelineTrace{
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: t0, Desired: 0},
			{Timestamp: t0.Add(60 * time.Second), Desired: 50},
			{Timestamp: t0.Add(120 * time.Second), Desired: 8},
		},
	}
	minReplicas := int32(2)
	candidate := replayCandidateConfig{
		MinReplicas:                   &minReplicas,
		MaxReplicas:                   10,
		ScaleDownStabilizationSeconds: 0,
	}
	out := applyReplayCandidate(original, candidate)

	// Original must not be mutated.
	if original.Snapshots[0].Desired != 0 || original.Snapshots[1].Desired != 50 {
		t.Fatalf("applyReplayCandidate mutated the input trace: %+v", original.Snapshots)
	}
	// minReplicas clamp applies to the zero desired.
	if out.Snapshots[0].Desired != 2 {
		t.Fatalf("clamped min expected 2, got %d", out.Snapshots[0].Desired)
	}
	// maxReplicas clamp applies to 50 and marks as LIMITED.
	if out.Snapshots[1].Desired != 10 {
		t.Fatalf("clamped max expected 10, got %d", out.Snapshots[1].Desired)
	}
	if out.Snapshots[1].Health != "LIMITED" {
		t.Fatalf("expected clamped snapshot health LIMITED, got %q", out.Snapshots[1].Health)
	}
	// Within bounds stays unchanged.
	if out.Snapshots[2].Desired != 8 {
		t.Fatalf("in-range desired expected 8, got %d", out.Snapshots[2].Desired)
	}
}

func TestApplyReplayCandidateScaleDownStabilization(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trace := hpaanalysis.TimelineTrace{
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: t0, Desired: 10},
			{Timestamp: t0.Add(30 * time.Second), Desired: 5},   // within 90s window → held at 10
			{Timestamp: t0.Add(120 * time.Second), Desired: 5},  // beyond 90s window → allowed to drop
		},
	}
	candidate := replayCandidateConfig{
		MaxReplicas:                   100,
		ScaleDownStabilizationSeconds: 90,
	}
	out := applyReplayCandidate(trace, candidate)
	if out.Snapshots[1].Desired != 10 {
		t.Fatalf("stabilization should hold desired at 10, got %d", out.Snapshots[1].Desired)
	}
	if out.Snapshots[2].Desired != 5 {
		t.Fatalf("beyond stabilization window desired should drop to 5, got %d", out.Snapshots[2].Desired)
	}
}

func TestComputeReplayImpact(t *testing.T) {
	current := replayLabSummary{
		ScaleEvents:             10,
		PodHours:               100,
		EstimatedUnderProvision: 3,
		PeakReplicas:           8,
	}
	proposed := replayLabSummary{
		ScaleEvents:             4,
		PodHours:               80,
		EstimatedUnderProvision: 0,
		MaxReplicas:            12,
	}
	impact := computeReplayImpact(current, proposed)

	// 10 → 4: 60% reduction.
	wantReduction := (10.0 - 4.0) / 10.0 * 100
	if impact.ScaleEventReductionPct != wantReduction {
		t.Fatalf("ScaleEventReductionPct = %v, want %v", impact.ScaleEventReductionPct, wantReduction)
	}
	// 100 → 80 pod-hours: -20%.
	wantPodHoursChange := (80.0 - 100.0) / 100.0 * 100
	if impact.PodHoursChangePct != wantPodHoursChange {
		t.Fatalf("PodHoursChangePct = %v, want %v", impact.PodHoursChangePct, wantPodHoursChange)
	}
	if !impact.UnderProvisionFixed {
		t.Fatalf("UnderProvisionFixed expected true (3 → 0)")
	}
	if impact.AdditionalWorstCase != 4 {
		t.Fatalf("AdditionalWorstCase = %d, want 4 (proposed.MaxReplicas=12 - current.PeakReplicas=8)", impact.AdditionalWorstCase)
	}
	if !impact.NoMissedScaleUp {
		t.Fatalf("NoMissedScaleUp expected true (proposed under-provision == 0)")
	}
}

func TestComputeReplayImpactNoOp(t *testing.T) {
	// When proposed is not an improvement, no impact flags should fire.
	current := replayLabSummary{ScaleEvents: 5, PodHours: 50, EstimatedUnderProvision: 2, PeakReplicas: 10}
	proposed := replayLabSummary{ScaleEvents: 5, PodHours: 50, EstimatedUnderProvision: 2, MaxReplicas: 10}
	impact := computeReplayImpact(current, proposed)
	if impact.ScaleEventReductionPct != 0 || impact.PodHoursChangePct != 0 || impact.UnderProvisionFixed || impact.NoMissedScaleUp {
		t.Fatalf("expected zeroed impact, got %+v", impact)
	}
	if impact.AdditionalWorstCase != 0 {
		t.Fatalf("AdditionalWorstCase = %d, want 0 when proposed.MaxReplicas <= current.PeakReplicas", impact.AdditionalWorstCase)
	}
}

func TestReplayLabRecommendation(t *testing.T) {
	t.Run("improvement detected", func(t *testing.T) {
		current := replayLabSummary{ScaleEvents: 10, EstimatedUnderProvision: 3}
		candidate := replayLabSummary{ScaleEvents: 4, EstimatedUnderProvision: 1, AdditionalWorstCasePods: 2}
		got := replayLabRecommendation(current, candidate)
		if got == "" {
			t.Fatalf("expected non-empty recommendation")
		}
		// Should mention the churn reduction and the extra pods caveat.
		if !contains(got, "churn") || !contains(got, "+2") {
			t.Fatalf("recommendation missing expected phrases: %q", got)
		}
	})
	t.Run("no improvement", func(t *testing.T) {
		current := replayLabSummary{ScaleEvents: 5, EstimatedUnderProvision: 0}
		candidate := replayLabSummary{ScaleEvents: 5, EstimatedUnderProvision: 0, AdditionalWorstCasePods: 0}
		got := replayLabRecommendation(current, candidate)
		if got == "" {
			t.Fatalf("expected fallback recommendation text")
		}
		if contains(got, "reduces") {
			t.Fatalf("fallback should not claim reduction: %q", got)
		}
	})
}

// contains is a tiny test helper to avoid pulling strings just for substring checks.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
