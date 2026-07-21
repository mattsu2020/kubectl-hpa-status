package hpa

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteBehaviorAdvisorText_NilResult(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBehaviorAdvisorText(&buf, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output for nil result, got %q", buf.String())
	}
}

func TestWriteBehaviorAdvisorText_NoFindings(t *testing.T) {
	var buf bytes.Buffer
	result := &BehaviorAdvisorResult{Summary: "all good"}
	if err := WriteBehaviorAdvisorText(&buf, result, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No behavior tuning recommendations.") {
		t.Fatalf("expected no-recommendations line, got %q", buf.String())
	}
}

func TestWriteBehaviorAdvisorText_WithFindings(t *testing.T) {
	var buf bytes.Buffer
	result := &BehaviorAdvisorResult{
		Findings: []BehaviorFinding{{
			ID:          "stabilization-window",
			Category:    "stabilization",
			Severity:    SeverityWarning,
			Message:     "scaleDown stabilization window is 0s",
			Current:     "0s",
			Recommended: "300s",
			Patch:       "kubectl patch hpa web ...",
		}},
		Summary: "1 suggestion",
	}
	if err := WriteBehaviorAdvisorText(&buf, result, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"[warning] scaleDown stabilization window is 0s",
		"Current: 0s → Recommended: 300s",
		"Patch: kubectl patch hpa web ...",
		"Summary: 1 suggestion",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestAppendBehaviorAdvisorText(t *testing.T) {
	lbls := resolveLabels(nil)

	// nil result appends nothing.
	var buf []byte
	AppendBehaviorAdvisorText(&buf, nil, lbls)
	if len(buf) != 0 {
		t.Fatalf("expected empty buffer for nil result, got %q", buf)
	}

	result := &BehaviorAdvisorResult{
		Findings: []BehaviorFinding{
			{ID: "a", Severity: SeverityInfo, Message: "info finding"},
			{ID: "b", Severity: SeverityWarning, Message: "with current", Current: "5", Recommended: "10", Patch: "patch-cmd"},
		},
		Summary: "2 findings",
	}
	AppendBehaviorAdvisorText(&buf, result, lbls)
	out := string(buf)
	for _, want := range []string{
		lbls.BehaviorAdvisor + ":",
		"[info] info finding",
		"Current: 5 → Recommended: 10",
		"Patch: patch-cmd",
		"Summary: 2 findings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
