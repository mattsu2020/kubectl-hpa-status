package autoscalermap

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func TestWriteAutoscalerMapText_Nil(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, nil, style.NewTheme(false)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output for nil map, got %q", buf.String())
	}
}

func TestWriteAutoscalerMapText_Full(t *testing.T) {
	am := &Map{
		Namespace: "default",
		HPAName:   "web",
		Layers: []Layer{
			{Name: "hpa", Status: "scaling active", Healthy: true, Details: []string{"cpu target 70%"}},
			{Name: "nodes", Status: "unschedulable pressure", Healthy: false},
		},
		Risk: "high",
		Blockers: []Blocker{{
			Layer:    "nodes",
			Severity: "high",
			Message:  "insufficient cpu on nodes",
			Detail:   strings.Repeat("detail word ", 20),
		}},
		Recommendation: "add node capacity or lower requests",
		NextActions:    []string{"kubectl describe nodes"},
		NextChecks:     []string{"kubectl top nodes"},
	}

	var buf bytes.Buffer
	if err := WriteText(&buf, am, style.NewTheme(false)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Autoscaler map for default/web",
		"✓ hpa: scaling active",
		"✗ nodes: unschedulable pressure",
		"Risk: high",
		"Blockers:",
		"[HIGH] [nodes] insufficient cpu on nodes",
		"Recommendation:",
		"add node capacity or lower requests",
		"Next actions:",
		"- kubectl describe nodes",
		"Next checks:",
		"- kubectl top nodes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestAutoscalerBadges(t *testing.T) {
	theme := style.NewTheme(false)
	if got := autoscalerRiskBadge("high", theme); got != "high" {
		t.Errorf("risk badge high = %q", got)
	}
	if got := autoscalerRiskBadge("none", theme); got != "none" {
		t.Errorf("risk badge fallback = %q", got)
	}
	if got := autoscalerBlockerBadge("medium", theme); got != "[MED]" {
		t.Errorf("blocker badge medium = %q", got)
	}
	if got := autoscalerBlockerBadge("unknown", theme); got != "[INFO]" {
		t.Errorf("blocker badge fallback = %q", got)
	}
}

func TestWrapAutoscalerMapLines(t *testing.T) {
	if got := wrapAutoscalerMapLines("no wrap needed", 100); len(got) != 1 || got[0] != "no wrap needed" {
		t.Errorf("short text should stay one line: %v", got)
	}
	if got := wrapAutoscalerMapLines("anything", 0); len(got) != 1 || got[0] != "anything" {
		t.Errorf("zero width should pass through: %v", got)
	}
	if got := wrapAutoscalerMapLines("   ", 40); len(got) != 0 {
		t.Errorf("blank text should wrap to nothing: %v", got)
	}
	got := wrapAutoscalerMapLines("one two three four", 7)
	want := []string{"one two", "three", "four"}
	if len(got) != len(want) {
		t.Fatalf("wrap result = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wrap result = %v, want %v", got, want)
		}
	}
}
