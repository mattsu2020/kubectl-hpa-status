package hpa

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildChurnTestHPA() *autoscalingv2.HorizontalPodAutoscaler {
	min := int32(1)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "test"},
			MinReplicas:    &min,
			MaxReplicas:    10,
		},
	}
}

func rescaleEvent(to int32, ts time.Time) Event {
	return Event{
		Reason:    "SuccessfulRescale",
		Message:   fmt.Sprintf("New size: %d; reason: cpu", to),
		Timestamp: ts,
	}
}

func snapshot(desired int32, ts time.Time) TimelineSnapshot {
	return TimelineSnapshot{
		Current: desired, Desired: desired,
		Health: "OK", Timestamp: ts,
	}
}

func TestAnalyzeChurnFromEvents(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		events     []Event
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
			events: []Event{
				rescaleEvent(5, now.Add(-2*time.Minute)),
				rescaleEvent(3, now.Add(-1*time.Minute)),
			},
			wantNil: true,
		},
		{
			name: "monotonic scale-up returns LOW churn",
			events: []Event{
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
			events: []Event{
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
			name: "triple direction flip returns HIGH",
			events: []Event{
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
			events: []Event{
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
			name: "non-rescale events are ignored",
			events: []Event{
				{Reason: "FailedGetResourceMetric", Message: "missing metrics", Timestamp: now.Add(-3 * time.Minute)},
				rescaleEvent(3, now.Add(-2 * time.Minute)),
				{Reason: "SomethingElse", Message: "noise", Timestamp: now.Add(-90 * time.Second)},
				rescaleEvent(5, now.Add(-1 * time.Minute)),
				rescaleEvent(8, now.Add(-30 * time.Second)),
			},
			wantLevel: ChurnLow,
			wantFlips: 0,
		},
		{
			name: "events sorted by timestamp regardless of input order",
			events: []Event{
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
			events: []Event{
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
				if !types["tolerance"] {
					t.Error("expected tolerance recommendation")
				}
				if !types["behavior-policy"] {
					t.Error("expected behavior-policy recommendation")
				}
			},
		},
		{
			name: "patches are valid JSON",
			events: []Event{
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

func TestAnalyzeChurnFromSnapshots(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		snapshots []TimelineSnapshot
		wantNil   bool
		wantLevel ChurnLevel
	}{
		{
			name:      "nil snapshots returns nil",
			snapshots: nil,
			wantNil:   true,
		},
		{
			name: "fewer than 3 snapshots returns nil",
			snapshots: []TimelineSnapshot{
				snapshot(5, now.Add(-2*time.Minute)),
				snapshot(3, now.Add(-1*time.Minute)),
			},
			wantNil: true,
		},
		{
			name: "monotonic scale returns LOW",
			snapshots: []TimelineSnapshot{
				snapshot(3, now.Add(-3*time.Minute)),
				snapshot(5, now.Add(-2*time.Minute)),
				snapshot(8, now.Add(-1*time.Minute)),
			},
			wantLevel: ChurnLow,
		},
		{
			name: "oscillating snapshots return HIGH",
			snapshots: []TimelineSnapshot{
				snapshot(3, now.Add(-5*time.Minute)),
				snapshot(5, now.Add(-4*time.Minute)),
				snapshot(3, now.Add(-3*time.Minute)),
				snapshot(5, now.Add(-2*time.Minute)),
				snapshot(3, now.Add(-1*time.Minute)),
			},
			wantLevel: ChurnHigh,
		},
		{
			name: "rapid oscillation returns CRITICAL",
			snapshots: []TimelineSnapshot{
				snapshot(3, now.Add(-8*time.Minute)),
				snapshot(5, now.Add(-7*time.Minute)),
				snapshot(3, now.Add(-6*time.Minute)),
				snapshot(5, now.Add(-5*time.Minute)),
				snapshot(3, now.Add(-4*time.Minute)),
				snapshot(5, now.Add(-3*time.Minute)),
				snapshot(3, now.Add(-2*time.Minute)),
				snapshot(5, now.Add(-1*time.Minute)),
			},
			wantLevel: ChurnCritical,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AnalyzeChurnFromSnapshots(tc.snapshots, buildChurnTestHPA())
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
		})
	}
}
