package hpa

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

func TestAnalyzeFlappingPrevention(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name   string
		events []Event
		hpa    func() *interface { /* placeholder; we build *autoscalingv2.HPA via kube.BuildHPA */
		}
		wantNil    bool
		checkExtra func(t *testing.T, got *FlappingPreventionReport)
	}{
		{
			name:    "nil events returns nil",
			events:  nil,
			wantNil: true,
		},
		{
			name:    "empty events returns nil",
			events:  []Event{},
			wantNil: true,
		},
		{
			name: "fewer than 3 rescale events returns nil",
			events: []Event{
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			wantNil: true,
		},
		{
			name: "2 rescale events plus noise returns nil",
			events: []Event{
				{Reason: "FailedGetResourceMetric", Message: "missing metrics", Timestamp: now.Add(-3 * time.Minute)},
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			wantNil: true,
		},
		{
			name: "alternating flapping pattern produces recommendations with larger windows",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
				rescaleEvent(5, now.Add(-3*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				if got.CurrentDirectionFlips == 0 {
					t.Fatal("expected direction flips for alternating pattern")
				}
				if len(got.Recommendations) == 0 {
					t.Fatal("expected recommendations for flapping pattern")
				}
				for _, rec := range got.Recommendations {
					if rec.WindowSeconds <= got.CurrentWindow {
						t.Errorf("recommended window %ds should be larger than current %ds",
							rec.WindowSeconds, got.CurrentWindow)
					}
				}
			},
		},
		{
			name: "stable HPA with no direction flips has zero flips",
			events: []Event{
				rescaleEvent(3, now.Add(-4*time.Minute)),
				rescaleEvent(5, now.Add(-3*time.Minute)),
				rescaleEvent(8, now.Add(-2*time.Minute)),
				rescaleEvent(10, now.Add(-1*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				if got.CurrentDirectionFlips != 0 {
					t.Fatalf("expected 0 flips for monotonic scaling, got %d", got.CurrentDirectionFlips)
				}
			},
		},
		{
			name: "estimated flap reduction is between 0 and 100",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				for _, rec := range got.Recommendations {
					if rec.EstimatedFlapReduction < 0 || rec.EstimatedFlapReduction > 100 {
						t.Errorf("flap reduction %.1f out of range [0, 100] for window %ds",
							rec.EstimatedFlapReduction, rec.WindowSeconds)
					}
				}
			},
		},
		{
			name: "patch format is valid JSON",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				for _, rec := range got.Recommendations {
					if rec.Patch == "" {
						continue
					}
					if !json.Valid([]byte(rec.Patch)) {
						t.Errorf("invalid JSON patch for window %ds: %s", rec.WindowSeconds, rec.Patch)
					}
				}
			},
		},
		{
			name: "recommendations sorted by estimated reduction descending",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
				rescaleEvent(5, now.Add(-3*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				for i := 1; i < len(got.Recommendations); i++ {
					if got.Recommendations[i].EstimatedFlapReduction > got.Recommendations[i-1].EstimatedFlapReduction {
						t.Errorf("recommendations not sorted by reduction: [%d]=%.1f > [%d]=%.1f",
							i, got.Recommendations[i].EstimatedFlapReduction,
							i-1, got.Recommendations[i-1].EstimatedFlapReduction)
					}
				}
			},
		},
		{
			name: "confidence is a string field with valid values",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				validConfidences := map[string]bool{"high": true, "medium": true, "low": true}
				for _, rec := range got.Recommendations {
					if !validConfidences[rec.Confidence] {
						t.Errorf("invalid confidence %q for window %ds", rec.Confidence, rec.WindowSeconds)
					}
				}
			},
		},
		{
			name: "non-rescale events are ignored",
			events: []Event{
				{Reason: "FailedGetResourceMetric", Message: "missing metrics", Timestamp: now.Add(-3 * time.Minute)},
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				{Reason: "SomethingElse", Message: "noise", Timestamp: now.Add(-90 * time.Second)},
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				if got.CurrentDirectionFlips < 3 {
					t.Fatalf("expected at least 3 flips for alternating pattern, got %d", got.CurrentDirectionFlips)
				}
			},
		},
		{
			name: "report has observation window",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				if got.ObservationWindow == "" {
					t.Fatal("expected non-empty observation window")
				}
			},
		},
		{
			name: "report has summary",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				if got.Summary == "" {
					t.Fatal("expected non-empty summary")
				}
			},
		},
		{
			name: "custom stabilization window is reflected in report",
			events: []Event{
				rescaleEvent(3, now.Add(-8*time.Minute)),
				rescaleEvent(5, now.Add(-7*time.Minute)),
				rescaleEvent(3, now.Add(-6*time.Minute)),
				rescaleEvent(5, now.Add(-5*time.Minute)),
				rescaleEvent(3, now.Add(-4*time.Minute)),
			},
			checkExtra: func(t *testing.T, got *FlappingPreventionReport) {
				if got.CurrentWindow != 120 {
					t.Fatalf("expected current window 120s, got %ds", got.CurrentWindow)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := kube.BuildHPA("default", "test-hpa")
			if tc.name == "custom stabilization window is reflected in report" {
				hpa = kube.BuildHPA("default", "test-hpa", kube.WithScaleDownStabilizationWindow(120))
			}

			got := AnalyzeFlappingPrevention(tc.events, hpa)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if tc.checkExtra != nil {
				tc.checkExtra(t, got)
			}
		})
	}
}

func TestComputeFlapReduction(t *testing.T) {
	tests := []struct {
		name           string
		currentFlips   int
		remainingFlips int
		want           float64
	}{
		{name: "zero flips returns zero", currentFlips: 0, remainingFlips: 0, want: 0},
		{name: "full reduction", currentFlips: 4, remainingFlips: 0, want: 100},
		{name: "half reduction", currentFlips: 4, remainingFlips: 2, want: 50},
		{name: "no reduction", currentFlips: 4, remainingFlips: 4, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeFlapReduction(tc.currentFlips, tc.remainingFlips)
			if got != tc.want {
				t.Fatalf("expected %.1f, got %.1f", tc.want, got)
			}
		})
	}
}

func TestBuildCandidateWindows(t *testing.T) {
	candidates := buildCandidateWindows(300)
	for _, c := range candidates {
		if c == 300 {
			t.Errorf("current window 300 should not appear in candidates, got %d", c)
		}
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate window")
	}

	prev := int32(0)
	for _, c := range candidates {
		if c <= prev {
			t.Errorf("candidates not sorted: %d <= %d", c, prev)
		}
		prev = c
	}
}

func TestCountDirectionFlips(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		rescales  []rescaleData
		wantFlips int
	}{
		{
			name: "monotonic up has zero flips",
			rescales: []rescaleData{
				{timestamp: now.Add(-3 * time.Minute), newSize: 3},
				{timestamp: now.Add(-2 * time.Minute), newSize: 5},
				{timestamp: now.Add(-1 * time.Minute), newSize: 8},
			},
			wantFlips: 0,
		},
		{
			name: "one flip up-down",
			rescales: []rescaleData{
				{timestamp: now.Add(-3 * time.Minute), newSize: 3},
				{timestamp: now.Add(-2 * time.Minute), newSize: 5},
				{timestamp: now.Add(-1 * time.Minute), newSize: 3},
			},
			wantFlips: 1,
		},
		{
			name: "multiple flips",
			rescales: []rescaleData{
				{timestamp: now.Add(-5 * time.Minute), newSize: 3},
				{timestamp: now.Add(-4 * time.Minute), newSize: 5},
				{timestamp: now.Add(-3 * time.Minute), newSize: 3},
				{timestamp: now.Add(-2 * time.Minute), newSize: 5},
				{timestamp: now.Add(-1 * time.Minute), newSize: 3},
			},
			wantFlips: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countDirectionFlips(tc.rescales)
			if got != tc.wantFlips {
				t.Fatalf("expected %d flips, got %d", tc.wantFlips, got)
			}
		})
	}
}
