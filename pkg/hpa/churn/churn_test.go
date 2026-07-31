package churn

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func buildChurnTestHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return testutil.BuildHPA("default", "test-hpa",
		testutil.WithMinMax(1, 10),
		testutil.WithScaleTargetRef("Deployment", "test"),
	)
}

func rescaleEvent(to int32, ts time.Time) event.Event {
	return event.Event{
		Reason:    "SuccessfulRescale",
		Message:   fmt.Sprintf("New size: %d; reason: cpu", to),
		Timestamp: ts,
	}
}

func TestAnalyzeChurnFromEvents(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		events     []event.Event
		wantNil    bool
		wantLevel  ChurnLevel
		wantFlips  int
		checkExtra func(t *testing.T, got *ChurnAnalysis)
	}{
		{
			name:    "nil events returns nil",
			events:  nil,
			wantNil: true,
		},
		{
			name: "fewer than 3 rescale events returns nil",
			events: []event.Event{
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			wantNil: true,
		},
		{
			name: "monotonic scale-up returns LOW churn",
			events: []event.Event{
				rescaleEvent(3, now.Add(-4*time.Minute)),
				rescaleEvent(5, now.Add(-3*time.Minute)),
				rescaleEvent(8, now.Add(-2*time.Minute)),
				rescaleEvent(10, now.Add(-1*time.Minute)),
			},
			wantLevel: ChurnLow,
			wantFlips: 0,
		},
		{
			name: "single direction flip returns MEDIUM or LOW",
			events: []event.Event{
				rescaleEvent(3, now.Add(-3*time.Minute)),
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *ChurnAnalysis) {
				if got.Level != ChurnLow && got.Level != ChurnMedium {
					t.Fatalf("expected LOW or MEDIUM, got %s", got.Level)
				}
			},
		},
		{
			name: "parsed zero replica event participates in churn analysis",
			events: []event.Event{
				rescaleEvent(2, now.Add(-3*time.Minute)),
				rescaleEvent(0, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			wantFlips: 1,
			checkExtra: func(t *testing.T, got *ChurnAnalysis) {
				if got.ScaleDownCount != 1 || got.ScaleUpCount != 1 {
					t.Fatalf("scale counts = down %d, up %d; want 1 and 1",
						got.ScaleDownCount, got.ScaleUpCount)
				}
			},
		},
		{
			name: "triple direction flip returns HIGH",
			events: []event.Event{
				rescaleEvent(3, now.Add(-5*time.Minute)),
				rescaleEvent(5, now.Add(-4*time.Minute)),
				rescaleEvent(3, now.Add(-3*time.Minute)),
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			wantLevel: ChurnHigh,
			wantFlips: 3,
		},
		{
			name: "rapid oscillation returns CRITICAL",
			events: []event.Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
				rescaleEvent(5, now.Add(-3*time.Minute)),
				rescaleEvent(3, now.Add(-2*time.Minute)),
				rescaleEvent(5, now.Add(-1*time.Minute)),
			},
			wantLevel: ChurnCritical,
			wantFlips: 6,
		},
		{
			name: "slow oscillation is discounted by activity rate",
			events: []event.Event{
				rescaleEvent(2, now.Add(-150*time.Minute)),
				rescaleEvent(8, now.Add(-120*time.Minute)),
				rescaleEvent(2, now.Add(-90*time.Minute)),
				rescaleEvent(9, now.Add(-60*time.Minute)),
				rescaleEvent(2, now.Add(-30*time.Minute)),
				rescaleEvent(10, now),
			},
			wantLevel: ChurnMedium,
			wantFlips: 4,
		},
		{
			name: "widely separated rescales are not a continuous churn burst",
			events: []event.Event{
				rescaleEvent(2, now.Add(-150*24*time.Hour)),
				rescaleEvent(8, now.Add(-120*24*time.Hour)),
				rescaleEvent(2, now.Add(-90*24*time.Hour)),
				rescaleEvent(9, now.Add(-60*24*time.Hour)),
				rescaleEvent(2, now.Add(-30*24*time.Hour)),
				rescaleEvent(10, now),
			},
			wantNil: true,
		},
		{
			name: "non-rescale events are ignored",
			events: []event.Event{
				{Reason: "FailedGetResourceMetric", Message: "missing metrics", Timestamp: now.Add(-3 * time.Minute)},
				rescaleEvent(3, now.Add(-2*time.Minute)),
				{Reason: "SomethingElse", Message: "noise", Timestamp: now.Add(-90 * time.Second)},
				rescaleEvent(5, now.Add(-1*time.Minute)),
				rescaleEvent(8, now.Add(-30*time.Second)),
			},
			wantLevel: ChurnLow,
			wantFlips: 0,
		},
		{
			name: "events sorted by timestamp regardless of input order",
			events: []event.Event{
				rescaleEvent(10, now.Add(-1*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
				rescaleEvent(5, now.Add(-3*time.Minute)),
				rescaleEvent(3, now.Add(-2*time.Minute)),
			},
			wantFlips: 2,
			checkExtra: func(t *testing.T, got *ChurnAnalysis) {
				if got.ScaleUpCount+got.ScaleDownCount != 3 {
					t.Fatalf("expected 3 direction changes, got up=%d down=%d", got.ScaleUpCount, got.ScaleDownCount)
				}
			},
		},
		{
			name: "recommendations generated for HIGH level",
			events: []event.Event{
				rescaleEvent(3, now.Add(-5*time.Minute)),
				rescaleEvent(5, now.Add(-4*time.Minute)),
				rescaleEvent(3, now.Add(-3*time.Minute)),
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *ChurnAnalysis) {
				if len(got.Recommendations) == 0 {
					t.Fatal("expected recommendations for HIGH churn")
				}
				types := make(map[string]bool)
				for _, r := range got.Recommendations {
					types[r.Type] = true
				}
				if !types["stabilization-window"] {
					t.Error("expected stabilization-window recommendation")
				}
				if types["tolerance"] {
					t.Error("feature-gated no-op tolerance recommendation must not be emitted")
				}
				if !types["behavior-policy"] {
					t.Error("expected behavior-policy recommendation")
				}
			},
		},
		{
			name: "patches are valid JSON",
			events: []event.Event{
				rescaleEvent(3, now.Add(-5*time.Minute)),
				rescaleEvent(5, now.Add(-4*time.Minute)),
				rescaleEvent(3, now.Add(-3*time.Minute)),
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *ChurnAnalysis) {
				for _, r := range got.Recommendations {
					if r.Patch == "" {
						continue
					}
					if !json.Valid([]byte(r.Patch)) {
						t.Errorf("invalid JSON patch for %s: %s", r.Type, r.Patch)
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AnalyzeChurnFromEvents(tc.events, buildChurnTestHPA())
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if tc.wantLevel != "" && got.Level != tc.wantLevel {
				t.Fatalf("expected level %s, got %s (score=%d)", tc.wantLevel, got.Level, got.Score)
			}
			if tc.wantFlips > 0 && got.DirectionFlips != tc.wantFlips {
				t.Fatalf("expected %d flips, got %d", tc.wantFlips, got.DirectionFlips)
			}
			if tc.checkExtra != nil {
				tc.checkExtra(t, got)
			}
		})
	}
}

// TestAnalyzeFromRescales_NilHPA guards the snapshot-replay path: history and
// replay viewers analyze recorded traces without a live HPA object, so a nil
// HPA must produce recommendations from defaults instead of panicking.
func TestAnalyzeFromRescales_NilHPA(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	var rescales []event.RescaleData
	for i, size := range []int32{2, 8, 2, 9, 2, 10} {
		rescales = append(rescales, event.RescaleData{
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			NewSize:   size,
		})
	}
	got := AnalyzeFromRescales(rescales, nil)
	if got == nil {
		t.Fatal("expected analysis for nil HPA")
	}
	if got.Level == ChurnLow {
		t.Fatalf("expected churn above LOW for oscillating trace, got %s", got.Level)
	}
	if len(got.Recommendations) == 0 {
		t.Fatal("expected recommendations built from default stabilization window")
	}
}

func TestAnalyzeFromRescales_DoesNotModifyInput(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	rescales := []event.RescaleData{
		{Timestamp: base.Add(2 * time.Minute), NewSize: 3},
		{Timestamp: base, NewSize: 2},
		{Timestamp: base.Add(time.Minute), NewSize: 5},
	}
	original := append([]event.RescaleData(nil), rescales...)

	if got := AnalyzeFromRescales(rescales, nil); got == nil {
		t.Fatal("expected analysis")
	}
	for i := range rescales {
		if rescales[i] != original[i] {
			t.Fatalf("input[%d] changed from %#v to %#v", i, original[i], rescales[i])
		}
	}
}

func TestNormalizeRescaleObservationsSameTimestamp(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	input := []event.RescaleData{
		{Timestamp: base.Add(time.Minute), NewSize: 5},
		{Timestamp: base, NewSize: 2},
		{Timestamp: base.Add(time.Minute), NewSize: 5},
		{Timestamp: base.Add(2 * time.Minute), NewSize: 3},
		{Timestamp: base.Add(3 * time.Minute), NewSize: 8},
		{Timestamp: base.Add(3 * time.Minute), NewSize: 4},
	}

	got := normalizeRescaleObservations(input)
	if len(got) != 3 {
		t.Fatalf("normalized observations = %#v, want 3 unambiguous timestamps", got)
	}
	for i, want := range []int32{2, 5, 3} {
		if got[i].NewSize != want {
			t.Fatalf("normalized[%d].NewSize = %d, want %d", i, got[i].NewSize, want)
		}
	}
}

func TestAnalyzeFromRescalesSameTimestampIsOrderIndependent(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	first := []event.RescaleData{
		{Timestamp: base, NewSize: 2},
		{Timestamp: base.Add(time.Minute), NewSize: 8},
		{Timestamp: base.Add(time.Minute), NewSize: 3},
		{Timestamp: base.Add(2 * time.Minute), NewSize: 5},
		{Timestamp: base.Add(3 * time.Minute), NewSize: 2},
		{Timestamp: base.Add(4 * time.Minute), NewSize: 6},
	}
	second := append([]event.RescaleData(nil), first...)
	second[1], second[2] = second[2], second[1]

	gotFirst := AnalyzeFromRescales(first, buildChurnTestHPA())
	gotSecond := AnalyzeFromRescales(second, buildChurnTestHPA())
	if gotFirst == nil || gotSecond == nil {
		t.Fatalf("expected both analyses, got first=%+v second=%+v", gotFirst, gotSecond)
	}
	if gotFirst.DirectionFlips != 2 || gotFirst.ScaleUpCount != 2 || gotFirst.ScaleDownCount != 1 {
		t.Fatalf("ambiguous timestamp affected transitions: %+v", gotFirst)
	}
	if gotFirst.DirectionFlips != gotSecond.DirectionFlips ||
		gotFirst.ScaleUpCount != gotSecond.ScaleUpCount ||
		gotFirst.ScaleDownCount != gotSecond.ScaleDownCount ||
		gotFirst.Score != gotSecond.Score ||
		gotFirst.TimeWindow != gotSecond.TimeWindow {
		t.Fatalf("same-timestamp order changed analysis: first=%+v second=%+v", gotFirst, gotSecond)
	}
}

func TestNextStabilizationWindowSeconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current int32
		want    int32
		wantOK  bool
	}{
		{name: "disabled starts at 300", current: 0, want: 300, wantOK: true},
		{name: "negative defensive fallback starts at 300", current: -1, want: 300, wantOK: true},
		{name: "positive value doubles", current: 300, want: 600, wantOK: true},
		{name: "half maximum doubles to maximum", current: 1800, want: 3600, wantOK: true},
		{name: "doubling is clamped", current: 2000, want: 3600, wantOK: true},
		{name: "maximum has no recommendation", current: 3600, wantOK: false},
		{name: "above maximum has no recommendation", current: 4000, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := nextStabilizationWindowSeconds(tc.current)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("nextStabilizationWindowSeconds(%d) = (%d, %t), want (%d, %t)",
					tc.current, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestStabilizationWindowRecommendationUsesValidBounds(t *testing.T) {
	t.Parallel()
	window := int32(0)
	hpa := buildChurnTestHPA()
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: &window},
	}

	recommendation, ok := stabilizationWindowRecommendation(hpa)
	if !ok {
		t.Fatal("expected recommendation for disabled stabilization")
	}
	if recommendation.CurrentValue != "0s" || recommendation.RecommendedValue != "300s" {
		t.Fatalf("unexpected recommendation: %+v", recommendation)
	}
	var patch struct {
		Spec struct {
			Behavior struct {
				ScaleDown struct {
					Window int32 `json:"stabilizationWindowSeconds"`
				} `json:"scaleDown"`
			} `json:"behavior"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(recommendation.Patch), &patch); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patch.Spec.Behavior.ScaleDown.Window != 300 {
		t.Fatalf("patch window = %d, want 300", patch.Spec.Behavior.ScaleDown.Window)
	}

	window = 3600
	if recommendation, ok := stabilizationWindowRecommendation(hpa); ok {
		t.Fatalf("maximum window must not produce a recommendation: %+v", recommendation)
	}
	for _, recommendation := range generateChurnRecommendations(ChurnMedium, hpa) {
		if recommendation.Type == "stabilization-window" {
			t.Fatalf("maximum window leaked into generated recommendations: %+v", recommendation)
		}
	}
}

func TestBehaviorPolicyRecommendationIsConservative(t *testing.T) {
	t.Parallel()
	t.Run("no explicit policy gets a minimal patch", func(t *testing.T) {
		t.Parallel()
		recommendation := behaviorPolicyRecommendation(buildChurnTestHPA())
		if recommendation.Patch == "" {
			t.Fatal("expected a patch when no explicit scale-down policy exists")
		}
		if strings.Contains(recommendation.Patch, "selectPolicy") ||
			strings.Contains(recommendation.Patch, "stabilizationWindowSeconds") {
			t.Fatalf("policy patch changes unrelated behavior fields: %s", recommendation.Patch)
		}
		var patch struct {
			Spec struct {
				Behavior struct {
					ScaleDown struct {
						Policies []autoscalingv2.HPAScalingPolicy `json:"policies"`
					} `json:"scaleDown"`
				} `json:"behavior"`
			} `json:"spec"`
		}
		if err := json.Unmarshal([]byte(recommendation.Patch), &patch); err != nil {
			t.Fatalf("decode patch: %v", err)
		}
		policies := patch.Spec.Behavior.ScaleDown.Policies
		if len(policies) != 1 || policies[0].Type != autoscalingv2.PercentScalingPolicy ||
			policies[0].Value != 50 || policies[0].PeriodSeconds != 60 {
			t.Fatalf("unexpected policy patch: %+v", policies)
		}
	})

	t.Run("existing stricter policy is never replaced", func(t *testing.T) {
		t.Parallel()
		hpa := buildChurnTestHPA()
		hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{
				Policies: []autoscalingv2.HPAScalingPolicy{{
					Type: autoscalingv2.PercentScalingPolicy, Value: 10, PeriodSeconds: 60,
				}},
			},
		}
		recommendation := behaviorPolicyRecommendation(hpa)
		if recommendation.Patch != "" {
			t.Fatalf("existing policy must receive patch-free guidance, got %s", recommendation.Patch)
		}
		if !strings.Contains(recommendation.CurrentValue, "10%/60s") {
			t.Fatalf("existing policy is not reflected in guidance: %+v", recommendation)
		}
	})

	t.Run("disabled scale-down is never enabled", func(t *testing.T) {
		t.Parallel()
		disabled := autoscalingv2.DisabledPolicySelect
		hpa := buildChurnTestHPA()
		hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{SelectPolicy: &disabled},
		}
		recommendation := behaviorPolicyRecommendation(hpa)
		if recommendation.Patch != "" {
			t.Fatalf("disabled scale-down must receive patch-free guidance, got %s", recommendation.Patch)
		}
		if !strings.Contains(recommendation.CurrentValue, "disabled") {
			t.Fatalf("disabled policy is not reflected in guidance: %+v", recommendation)
		}
	})
}

func TestHighChurnDoesNotEmitTolerancePatch(t *testing.T) {
	t.Parallel()
	for _, recommendation := range generateChurnRecommendations(ChurnHigh, buildChurnTestHPA()) {
		if recommendation.Type == "tolerance" || strings.Contains(recommendation.Patch, "tolerance") {
			t.Fatalf("feature-gated/no-op tolerance recommendation emitted: %+v", recommendation)
		}
	}
}
