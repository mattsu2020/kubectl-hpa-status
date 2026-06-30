package tui

import (
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestClampCursor(t *testing.T) {
	tests := []struct {
		name  string
		v, hi int
		want  int
	}{
		{name: "in range returns v", v: 3, hi: 10, want: 3},
		{name: "negative v clamps to zero", v: -5, hi: 10, want: 0},
		{name: "v above hi clamps to hi", v: 15, hi: 10, want: 10},
		{name: "negative hi returns zero", v: 3, hi: -1, want: 0},
		{name: "zero v zero hi returns zero", v: 0, hi: 0, want: 0},
		{name: "v equals hi returns hi", v: 5, hi: 5, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampCursor(tt.v, tt.hi); got != tt.want {
				t.Fatalf("clampCursor(%d, %d) = %d, want %d", tt.v, tt.hi, got, tt.want)
			}
		})
	}
}

func TestRenderStabBadge(t *testing.T) {
	t.Run("non-stabilizing item renders dash", func(t *testing.T) {
		item := hpaanalysis.ListItem{Stabilizing: false}
		got := renderStabBadge(item, 6)
		if !strings.Contains(got, "-") {
			t.Fatalf("expected dash for non-stabilizing item, got: %q", got)
		}
	})

	t.Run("stabilizing without label renders dash", func(t *testing.T) {
		item := hpaanalysis.ListItem{Stabilizing: true, StabilizationLabel: ""}
		got := renderStabBadge(item, 6)
		if !strings.Contains(got, "-") {
			t.Fatalf("expected dash for empty label, got: %q", got)
		}
	})

	t.Run("stabilizing with label renders label", func(t *testing.T) {
		item := hpaanalysis.ListItem{Stabilizing: true, StabilizationLabel: "30s"}
		got := renderStabBadge(item, 10)
		if !strings.Contains(got, "30s") {
			t.Fatalf("expected label '30s' in badge, got: %q", got)
		}
	})

	t.Run("label truncated to width", func(t *testing.T) {
		item := hpaanalysis.ListItem{Stabilizing: true, StabilizationLabel: "very-long-label"}
		got := renderStabBadge(item, 4)
		if !strings.Contains(got, "very") {
			t.Fatalf("expected truncated label 'very', got: %q", got)
		}
	})
}

func TestRenderInlineSparkline(t *testing.T) {
	t.Run("short history renders placeholder", func(t *testing.T) {
		got := renderInlineSparkline([]float64{1.0}, 8, "")
		if !strings.Contains(got, "·") {
			t.Fatalf("expected placeholder for short history, got: %q", got)
		}
	})

	t.Run("nil history renders placeholder", func(t *testing.T) {
		got := renderInlineSparkline(nil, 8, "")
		if !strings.Contains(got, "·") {
			t.Fatalf("expected placeholder for nil history, got: %q", got)
		}
	})

	t.Run("sufficient history renders bars", func(t *testing.T) {
		// Two points is the minimum for a sparkline; it must not render the
		// placeholder. The exact glyph set is style-dependent, so we only
		// assert the placeholder is absent.
		history := []float64{1.0, 2.0, 3.0, 4.0}
		got := renderInlineSparkline(history, 8, "HIGH")
		if strings.Contains(got, "·") {
			t.Fatalf("expected sparkline bars, got placeholder: %q", got)
		}
	})
}

func TestAppendMetricsReportBody(t *testing.T) {
	t.Run("no metrics renders placeholder message", func(t *testing.T) {
		var sb strings.Builder
		appendMetricsReportBody(&sb, hpaanalysis.Analysis{})
		if !strings.Contains(sb.String(), "No metrics configured") {
			t.Fatalf("expected no-metrics placeholder, got: %q", sb.String())
		}
	})

	t.Run("metrics render without panic", func(t *testing.T) {
		var sb strings.Builder
		a := hpaanalysis.Analysis{
			Metrics: []hpaanalysis.Metric{
				{Name: "cpu", Target: "70%", Current: "65%", Type: "Resource"},
			},
		}
		appendMetricsReportBody(&sb, a)
		// Must mention the metric name and not include the no-metrics message.
		if !strings.Contains(sb.String(), "cpu") {
			t.Fatalf("expected metric name in output, got: %q", sb.String())
		}
		if strings.Contains(sb.String(), "No metrics configured") {
			t.Fatalf("did not expect no-metrics placeholder when metrics present")
		}
	})
}
