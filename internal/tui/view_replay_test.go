package tui

import (
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// TestFormatBottleneckSummary covers the bottleneck summary formatter that was
// only exercised indirectly through Model.View() before.
func TestFormatBottleneckSummary(t *testing.T) {
	tests := []struct {
		name            string
		highCount       int
		medCount        int
		total           int
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:         "both high and med",
			highCount:    2,
			medCount:     3,
			total:        5,
			wantContains: []string{"Bottlenecks: 5", "HIGH", "MED"},
		},
		{
			name:            "only high",
			highCount:       1,
			medCount:        0,
			total:           1,
			wantContains:    []string{"Bottlenecks: 1", "HIGH"},
			wantNotContains: []string{"MED"},
		},
		{
			name:            "only med",
			highCount:       0,
			medCount:        2,
			total:           2,
			wantContains:    []string{"Bottlenecks: 2", "MED"},
			wantNotContains: []string{"HIGH"},
		},
		{
			name:            "none",
			highCount:       0,
			medCount:        0,
			total:           0,
			wantContains:    []string{"Bottlenecks: 0"},
			wantNotContains: []string{"HIGH", "MED"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatBottleneckSummary(tc.highCount, tc.medCount, tc.total)
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in output, got %q", want, got)
				}
			}
			for _, notWant := range tc.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("expected %q NOT in output, got %q", notWant, got)
				}
			}
		})
	}
}

// TestRenderReplayView_TailScrollShowsFullPage locks in the tail-clamp
// behavior of the shared scrollWindow: moveReplayViewCursor allows scrollPos
// up to len(snapshots)-1, and the window must clamp to the last full page
// instead of shrinking to the few rows after scrollPos.
func TestRenderReplayView_TailScrollShowsFullPage(t *testing.T) {
	m := detailModel(Options{})
	m.viewMode = replayView

	const total = 40
	now := time.Date(2025, 1, 2, 3, 0, 0, 0, time.UTC)
	snapshots := make([]hpaanalysis.TimelineSnapshot, 0, total)
	for i := range total {
		snapshots = append(snapshots, hpaanalysis.TimelineSnapshot{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			Current:   2,
			Desired:   2,
			Health:    "OK",
			Summary:   "steady",
		})
	}

	m.replayState = &replayState{
		trace: &hpaanalysis.TimelineTrace{
			HPAName:   "web",
			Namespace: "default",
			Snapshots: snapshots,
		},
		scrollPos: total - 1, // deepest position moveReplayViewCursor allows
	}

	// visibleHeight is m.height-6 (40-6=34), so the clamped window is
	// rows 7..40 (indices 6..39), not the single tail row at scrollPos.
	out := m.renderReplayView()
	if !strings.Contains(out, "[7-40 of 40]") {
		t.Fatalf("expected clamped window indicator [7-40 of 40], got:\n%s", out)
	}
	if !strings.Contains(out, "03:06:00") {
		t.Error("expected first clamped row 03:06:00 in output")
	}
	if strings.Contains(out, "03:05:00") {
		t.Error("row 03:05:00 is outside the clamped window and must not render")
	}
	if !strings.Contains(out, "03:39:00") {
		t.Error("expected last row 03:39:00 in output")
	}
}
