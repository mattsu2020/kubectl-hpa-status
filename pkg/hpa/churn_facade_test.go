package hpa

import (
	"strconv"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func churnTestHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
		},
	}
}

func TestAnalyzeChurnFromEvents_NoRescales(t *testing.T) {
	if got := AnalyzeChurnFromEvents(nil, churnTestHPA()); got != nil {
		t.Fatalf("expected nil analysis without rescale events, got %+v", got)
	}
	// Non-rescale reasons are ignored.
	events := []Event{{Reason: "FailedGetMetrics", Message: "metrics unavailable"}}
	if got := AnalyzeChurnFromEvents(events, churnTestHPA()); got != nil {
		t.Fatalf("expected nil analysis for non-rescale events, got %+v", got)
	}
}

func TestAnalyzeChurnFromEvents_Thrashing(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(minutes int, size int) Event {
		return Event{
			Reason:    "SuccessfulRescale",
			Message:   "New size: " + strconv.Itoa(size) + "; reason: cpu resource utilization above target",
			Timestamp: base.Add(time.Duration(minutes) * time.Minute),
		}
	}
	events := []Event{mk(0, 2), mk(1, 5), mk(2, 2), mk(3, 6), mk(4, 2)}
	got := AnalyzeChurnFromEvents(events, churnTestHPA())
	if got == nil {
		t.Fatal("expected non-nil analysis for repeated rescales")
	}
	if got.Level == ChurnLow {
		t.Fatalf("Level = %v, want elevated churn for thrashing events", got.Level)
	}
}

func TestAnalyzeChurnFromSnapshots_Thrashing(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshots := []TimelineSnapshot{
		{Timestamp: base, Desired: 2},
		{Timestamp: base.Add(1 * time.Minute), Desired: 5},
		{Timestamp: base.Add(2 * time.Minute), Desired: 2},
		{Timestamp: base.Add(3 * time.Minute), Desired: 6},
		{Timestamp: base.Add(4 * time.Minute), Desired: 2},
		{Timestamp: base.Add(5 * time.Minute), Desired: 7},
	}
	got := AnalyzeChurnFromSnapshots(snapshots, churnTestHPA())
	if got == nil {
		t.Fatal("expected non-nil analysis")
	}
	if got.Level == ChurnLow {
		t.Fatalf("Level = %v, want elevated churn for thrashing snapshots", got.Level)
	}
}
