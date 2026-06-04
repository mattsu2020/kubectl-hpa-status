package hpa

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

func TestSnapshotFromReport(t *testing.T) {
	report := StatusReport{
		Analysis: Analysis{
			Namespace:   "default",
			Name:        "web",
			Current:     3,
			Desired:     5,
			Health:      "LIMITED",
			HealthScore: 75,
			Summary:     "Scaling up",
			Conditions: []Condition{
				{Type: "ScalingLimited", Status: "True", Reason: "LimitedByMaxReplicas"},
			},
			Interpretation: []string{"within tolerance"},
			ImpactMetric: &MetricImpactGuess{
				Name:  "cpu",
				Ratio: 1.42,
				Note:  "above target",
			},
		},
		Events: []Event{
			{Reason: "SuccessfulRescale", Message: "New size: 5"},
		},
	}

	snap := SnapshotFromReport(report)

	if snap.Current != 3 {
		t.Errorf("expected Current=3, got %d", snap.Current)
	}
	if snap.Desired != 5 {
		t.Errorf("expected Desired=5, got %d", snap.Desired)
	}
	if snap.Health != "LIMITED" {
		t.Errorf("expected Health=LIMITED, got %q", snap.Health)
	}
	if snap.HealthScore != 75 {
		t.Errorf("expected HealthScore=75, got %d", snap.HealthScore)
	}
	if !strings.Contains(snap.TopMetric, "cpu") {
		t.Errorf("expected TopMetric to contain 'cpu', got %q", snap.TopMetric)
	}
	if len(snap.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(snap.Conditions))
	}
	if len(snap.Interpretation) != 1 {
		t.Errorf("expected 1 interpretation, got %d", len(snap.Interpretation))
	}
	if len(snap.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(snap.Events))
	}
}

func TestDiffSnapshots_ReplicaChange(t *testing.T) {
	prev := TimelineSnapshot{
		Current: 3,
		Desired: 3,
		Health:  "OK",
	}
	curr := TimelineSnapshot{
		Current: 3,
		Desired: 5,
		Health:  "OK",
	}

	changes := DiffSnapshots(prev, curr)

	found := false
	for _, c := range changes {
		if strings.Contains(c, "replicas") && strings.Contains(c, "3/3") && strings.Contains(c, "3/5") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected replica change in diff, got %v", changes)
	}
}

func TestDiffSnapshots_HealthChange(t *testing.T) {
	prev := TimelineSnapshot{Health: "OK", HealthScore: 100}
	curr := TimelineSnapshot{Health: "LIMITED", HealthScore: 75}

	changes := DiffSnapshots(prev, curr)

	found := false
	for _, c := range changes {
		if strings.Contains(c, "health") && strings.Contains(c, "OK") && strings.Contains(c, "LIMITED") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected health change in diff, got %v", changes)
	}
}

func TestDiffSnapshots_NoChange(t *testing.T) {
	snap := TimelineSnapshot{
		Current:     3,
		Desired:     3,
		Health:      "OK",
		HealthScore: 100,
		Conditions:  []Condition{{Type: "ScalingActive", Status: "True"}},
	}

	changes := DiffSnapshots(snap, snap)

	if len(changes) != 0 {
		t.Errorf("expected no changes for identical snapshots, got %v", changes)
	}
}

func TestWriteTimelineTable(t *testing.T) {
	trace := TimelineTrace{
		HPAName:   "web",
		Namespace: "default",
		Start:     time.Now(),
		Interval:  5 * time.Second,
		Snapshots: []TimelineSnapshot{
			{
				Timestamp:   time.Now(),
				Current:     3,
				Desired:     3,
				Health:      "OK",
				HealthScore: 100,
				TopMetric:   "cpu (ratio=0.90 within target)",
				Summary:     "steady",
			},
			{
				Timestamp:   time.Now().Add(5 * time.Second),
				Current:     3,
				Desired:     5,
				Health:      "LIMITED",
				HealthScore: 75,
				TopMetric:   "memory (ratio=1.42 above target)",
				Summary:     "scaling up",
			},
		},
	}

	var buf bytes.Buffer
	theme := style.NewTheme(false)
	err := WriteTimelineTable(&buf, trace, theme)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Error("expected output to contain HPA name")
	}
	if !strings.Contains(output, "TIME") {
		t.Error("expected output to contain table header")
	}
	if !strings.Contains(output, "replicas") {
		t.Error("expected output to contain replica change")
	}
}

func TestWriteTimelineMarkdown(t *testing.T) {
	trace := TimelineTrace{
		HPAName:   "web",
		Namespace: "production",
		Start:     time.Now(),
		Interval:  5 * time.Second,
		Snapshots: []TimelineSnapshot{
			{
				Timestamp:   time.Now(),
				Current:     3,
				Desired:     5,
				Health:      "LIMITED",
				HealthScore: 75,
				TopMetric:   "cpu",
				Summary:     "scaling up",
			},
		},
	}

	var buf bytes.Buffer
	err := WriteTimelineMarkdown(&buf, trace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "# HPA Timeline") {
		t.Error("expected markdown header")
	}
	if !strings.Contains(output, "| Time |") {
		t.Error("expected markdown table header")
	}
	if !strings.Contains(output, "production") {
		t.Error("expected namespace in output")
	}
}

func TestWriteTimelineHTML(t *testing.T) {
	trace := TimelineTrace{
		HPAName:   "web",
		Namespace: "default",
		Start:     time.Now(),
		Snapshots: []TimelineSnapshot{
			{
				Timestamp:   time.Now(),
				Current:     3,
				Desired:     3,
				Health:      "OK",
				HealthScore: 100,
			},
		},
	}

	var buf bytes.Buffer
	err := WriteTimelineHTML(&buf, trace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<!DOCTYPE html>") {
		t.Error("expected HTML doctype")
	}
	if !strings.Contains(output, "<table>") {
		t.Error("expected HTML table")
	}
	if !strings.Contains(output, "web") {
		t.Error("expected HPA name in output")
	}
}

func TestSnapshotFromReport_EmptyMetrics(t *testing.T) {
	report := StatusReport{
		Analysis: Analysis{
			Current:  2,
			Desired:  2,
			Health:   "OK",
			HealthScore: 100,
		},
	}

	snap := SnapshotFromReport(report)

	if snap.TopMetric != "" {
		t.Errorf("expected empty TopMetric for no metrics, got %q", snap.TopMetric)
	}
}
