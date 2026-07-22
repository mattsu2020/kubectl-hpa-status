package healthtrend

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHealthTrendASCII_EmptySnapshots(t *testing.T) {
	result := RenderHealthTrendASCII(nil, 40)
	if result != "" {
		t.Errorf("expected empty string for nil snapshots, got %q", result)
	}

	result = RenderHealthTrendASCII([]HealthSnapshot{}, 40)
	if result != "" {
		t.Errorf("expected empty string for empty snapshots, got %q", result)
	}
}

func TestRenderHealthTrendASCII_ZeroOrNegativeWidth(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now, HealthScore: 80, HealthState: "OK"},
	}

	result := RenderHealthTrendASCII(snapshots, 0)
	if result != "" {
		t.Errorf("expected empty string for zero width, got %q", result)
	}

	result = RenderHealthTrendASCII(snapshots, -5)
	if result != "" {
		t.Errorf("expected empty string for negative width, got %q", result)
	}
}

func TestRenderHealthTrendASCII_SingleSnapshot(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now, HealthScore: 80, HealthState: "OK"},
	}

	result := RenderHealthTrendASCII(snapshots, 40)
	if result == "" {
		t.Fatal("expected non-empty graph for single snapshot")
	}

	if !strings.Contains(result, "•") {
		t.Error("expected graph to contain a data point marker (•)")
	}

	if !strings.Contains(result, "80") {
		t.Error("expected graph to contain score label 80")
	}
}

func TestRenderHealthTrendASCII_ExpectedLineCount(t *testing.T) {
	now := time.Now()
	snapshots := make([]HealthSnapshot, 10)
	for i := 0; i < 10; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Hour),
			HealthScore: 70 + i*3,
			HealthState: "OK",
		}
	}

	result := RenderHealthTrendASCII(snapshots, 40)
	if result == "" {
		t.Fatal("expected non-empty graph")
	}

	// graphYSteps rows + x-axis row + timestamp label row = 7 lines.
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	expectedLines := graphYSteps + 2 // +1 for axis, +1 for labels
	if len(lines) != expectedLines {
		t.Errorf("expected %d lines, got %d", expectedLines, len(lines))
	}
}

func TestRenderHealthTrendASCII_YAxisLabels(t *testing.T) {
	now := time.Now()
	snapshots := make([]HealthSnapshot, 5)
	for i := 0; i < 5; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   now.Add(time.Duration(i) * time.Hour),
			HealthScore: 90,
			HealthState: "OK",
		}
	}

	result := RenderHealthTrendASCII(snapshots, 40)

	if !strings.Contains(result, "100") {
		t.Error("expected graph to contain 100 y-axis label")
	}
	if !strings.Contains(result, "  0") {
		t.Error("expected graph to contain 0 y-axis label")
	}
}

func TestRenderHealthTrendASCII_XAxisTimestamps(t *testing.T) {
	// Use a fixed base time so the timestamps are deterministic.
	base := time.Date(2025, 6, 10, 6, 0, 0, 0, time.UTC)
	snapshots := make([]HealthSnapshot, 6)
	for i := 0; i < 6; i++ {
		snapshots[i] = HealthSnapshot{
			Timestamp:   base.Add(time.Duration(i) * time.Hour),
			HealthScore: 80,
			HealthState: "OK",
		}
	}

	result := RenderHealthTrendASCII(snapshots, 60)

	// Verify the output contains the first snapshot's timestamp.
	ts := snapshots[0].Timestamp.Format("15:04")
	if !strings.Contains(result, ts) {
		t.Errorf("expected graph to contain timestamp %q", ts)
	}
}

func TestRenderHealthTrendASCII_DataPointsRendered(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-3 * time.Hour), HealthScore: 100, HealthState: "OK"},
		{Timestamp: now.Add(-2 * time.Hour), HealthScore: 70, HealthState: "OK"},
		{Timestamp: now.Add(-1 * time.Hour), HealthScore: 40, HealthState: "LIMITED"},
		{Timestamp: now, HealthScore: 20, HealthState: "ERROR"},
	}

	result := RenderHealthTrendASCII(snapshots, 40)

	// Should have at least one data point marker.
	dotCount := strings.Count(result, "•")
	if dotCount == 0 {
		t.Error("expected at least one data point marker (•)")
	}
}

func TestRenderHealthTrendASCII_AnomalyMarkers(t *testing.T) {
	now := time.Now()

	// Create a series that will trigger sudden degradation.
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-6 * time.Minute), HealthScore: 90, HealthState: "OK"},
		{Timestamp: now.Add(-5 * time.Minute), HealthScore: 88, HealthState: "OK"},
		{Timestamp: now.Add(-4 * time.Minute), HealthScore: 85, HealthState: "OK"},
		{Timestamp: now.Add(-3 * time.Minute), HealthScore: 50, HealthState: "ERROR"},
		{Timestamp: now.Add(-2 * time.Minute), HealthScore: 48, HealthState: "ERROR"},
	}

	result := RenderHealthTrendASCII(snapshots, 40)

	// When anomalies are detected, the graph should contain anomaly markers.
	anomalies := DetectAnomalies(snapshots)
	if len(anomalies) > 0 {
		if !strings.Contains(result, "╳") {
			t.Error("expected graph to contain anomaly marker (╳) when anomalies are detected")
		}
	}
}

func TestRenderHealthTrendASCII_InputNotMutated(t *testing.T) {
	now := time.Now()
	snapshots := []HealthSnapshot{
		{Timestamp: now.Add(-2 * time.Hour), HealthScore: 90, HealthState: "OK"},
		{Timestamp: now.Add(-1 * time.Hour), HealthScore: 70, HealthState: "LIMITED"},
		{Timestamp: now, HealthScore: 50, HealthState: "ERROR"},
	}

	original := make([]HealthSnapshot, len(snapshots))
	copy(original, snapshots)

	_ = RenderHealthTrendASCII(snapshots, 40)

	for i := range snapshots {
		if snapshots[i] != original[i] {
			t.Errorf("snapshot[%d] was mutated: got %+v, want %+v", i, snapshots[i], original[i])
		}
	}
}
