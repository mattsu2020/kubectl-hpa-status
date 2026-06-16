package tui

import (
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestClampHintsSelection(t *testing.T) {
	tests := []struct {
		name      string
		selected  int
		flowCount int
		want      int
	}{
		{"negative clamps to zero", -3, 5, 0},
		{"zero stays zero", 0, 5, 0},
		{"mid-range unchanged", 2, 5, 2},
		{"at boundary unchanged", 4, 5, 4},
		{"above max clamps to last", 5, 5, 4},
		{"well above max clamps to last", 99, 5, 4},
		{"empty flows clamps to -1", 0, 0, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clampHintsSelection(tc.selected, tc.flowCount)
			if got != tc.want {
				t.Fatalf("clampHintsSelection(%d, %d) = %d, want %d", tc.selected, tc.flowCount, got, tc.want)
			}
		})
	}
}

func TestHintsStepWindow(t *testing.T) {
	tests := []struct {
		name        string
		stepScroll  int
		maxStep     int
		wantStart   int
		wantEnd     int
		wantVisible int
	}{
		{"negative scroll clamps to 0", -2, 3, 0, 3, 3},
		{"fewer than cap shows all", 0, 3, 0, 3, 3},
		{"exactly cap shows all", 0, 8, 0, 8, 8},
		{"more than cap truncates to 8", 0, 12, 0, 8, 8},
		{"scroll past end clamps back", 20, 12, 4, 12, 8},
		{"zero max", 0, 0, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, visible := hintsStepWindow(tc.stepScroll, tc.maxStep)
			if start != tc.wantStart || end != tc.wantEnd || visible != tc.wantVisible {
				t.Fatalf("hintsStepWindow(%d, %d) = (%d, %d, %d), want (%d, %d, %d)",
					tc.stepScroll, tc.maxStep, start, end, visible,
					tc.wantStart, tc.wantEnd, tc.wantVisible)
			}
		})
	}
}

func TestSeverityBadge(t *testing.T) {
	// All severities must produce a non-empty badge so the hint list never
	// drops a bullet point.
	for _, severity := range []string{"error", "warning", "info", "unknown", ""} {
		badge := severityBadge(severity)
		if badge == "" {
			t.Fatalf("severityBadge(%q) returned empty string", severity)
		}
	}
}

func TestAppendHintsList_SelectedHighlight(t *testing.T) {
	flows := []hpaanalysis.MetricHintTroubleshooting{
		{Title: "CPU missing", Severity: "error", MetricType: "Resource", MetricName: "cpu"},
		{Title: "Memory stale", Severity: "warning", MetricType: "Resource", MetricName: "memory"},
	}
	var sb strings.Builder
	appendHintsList(&sb, flows, 1)
	out := sb.String()
	if !contains(out, "CPU missing") || !contains(out, "Memory stale") {
		t.Fatalf("expected both hint titles in output, got:\n%s", out)
	}
}

func TestAppendHintsSteps_CommandAndDocs(t *testing.T) {
	flow := hpaanalysis.MetricHintTroubleshooting{
		Title: "CPU missing",
		Steps: []hpaanalysis.MetricHintFix{
			{StepNumber: 1, Description: "Check metrics server", Command: "kubectl top pods", ExpectedOutput: "CPU visible", DocsLink: "https://example.com"},
		},
	}
	var sb strings.Builder
	appendHintsSteps(&sb, flow, 0)
	out := sb.String()
	if !contains(out, "kubectl top pods") {
		t.Fatalf("expected command in output, got:\n%s", out)
	}
	if !contains(out, "expect: CPU visible") {
		t.Fatalf("expected expected-output line, got:\n%s", out)
	}
	if !contains(out, "docs: https://example.com") {
		t.Fatalf("expected docs link, got:\n%s", out)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
