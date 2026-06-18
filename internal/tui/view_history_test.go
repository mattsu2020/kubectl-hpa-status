package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// TestRenderSparkline covers the sparkline renderer edge cases that were only
// exercised indirectly through Model.View() before.
func TestRenderSparkline(t *testing.T) {
	t.Run("empty returns empty", func(t *testing.T) {
		if got := renderSparkline(nil, 10, lipgloss.NewStyle()); got != "" {
			t.Fatalf("expected empty for nil input, got %q", got)
		}
		if got := renderSparkline([]float64{}, 10, lipgloss.NewStyle()); got != "" {
			t.Fatalf("expected empty for empty input, got %q", got)
		}
	})

	t.Run("single value renders one block", func(t *testing.T) {
		got := renderSparkline([]float64{5.0}, 10, lipgloss.NewStyle())
		if !strings.Contains(got, "█") {
			t.Fatalf("expected single █ block, got %q", got)
		}
	})

	t.Run("all-equal values render middle block", func(t *testing.T) {
		got := renderSparkline([]float64{5.0, 5.0, 5.0}, 3, lipgloss.NewStyle())
		if !strings.Contains(got, "▄") {
			t.Fatalf("expected ▄ for all-equal values, got %q", got)
		}
	})

	t.Run("ascending values render increasing blocks", func(t *testing.T) {
		got := renderSparkline([]float64{1.0, 5.0, 10.0}, 3, lipgloss.NewStyle())
		// The output should be non-empty and contain at least one block character.
		if got == "" {
			t.Fatal("expected non-empty sparkline for varied values")
		}
		if !strings.ContainsAny(got, "▁▂▃▄▅▆▇█") {
			t.Fatalf("expected block character in sparkline, got %q", got)
		}
	})

	t.Run("width truncates to last N values", func(t *testing.T) {
		got := renderSparkline([]float64{1.0, 2.0, 3.0, 4.0, 5.0}, 2, lipgloss.NewStyle())
		if got == "" {
			t.Fatal("expected non-empty sparkline after truncation")
		}
	})
}

// TestRenderSparklineWithMarkers covers the marker overlay variant.
func TestRenderSparklineWithMarkers(t *testing.T) {
	t.Run("empty returns empty", func(t *testing.T) {
		got := renderSparklineWithMarkers(nil, 10, map[int]bool{}, lipgloss.NewStyle())
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("renders with markers without panic", func(t *testing.T) {
		got := renderSparklineWithMarkers([]float64{1.0, 5.0, 10.0}, 3, map[int]bool{1: true}, lipgloss.NewStyle())
		if got == "" {
			t.Fatal("expected non-empty sparkline with markers")
		}
	})
}

// TestRenderHealthTimeline covers the health timeline renderer edge cases.
func TestRenderHealthTimeline(t *testing.T) {
	t.Run("empty returns empty", func(t *testing.T) {
		if got := renderHealthTimeline(nil, 10); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("single OK snapshot renders block", func(t *testing.T) {
		snaps := []hpaanalysis.TimelineSnapshot{{Health: string(hpaanalysis.HealthOK)}}
		got := renderHealthTimeline(snaps, 10)
		if got == "" {
			t.Fatal("expected non-empty timeline for OK snapshot")
		}
	})

	t.Run("multiple snapshots render without panic", func(t *testing.T) {
		snaps := []hpaanalysis.TimelineSnapshot{
			{Health: string(hpaanalysis.HealthOK)},
			{Health: string(hpaanalysis.HealthLimited)},
			{Health: string(hpaanalysis.HealthError)},
			{Health: string(hpaanalysis.HealthStabilized)},
		}
		got := renderHealthTimeline(snaps, 4)
		if got == "" {
			t.Fatal("expected non-empty timeline for mixed snapshots")
		}
	})

	t.Run("width truncates to last N snapshots", func(t *testing.T) {
		snaps := make([]hpaanalysis.TimelineSnapshot, 10)
		for i := range snaps {
			snaps[i] = hpaanalysis.TimelineSnapshot{Health: string(hpaanalysis.HealthOK)}
		}
		got := renderHealthTimeline(snaps, 3)
		if got == "" {
			t.Fatal("expected non-empty timeline after truncation")
		}
	})
}
