package lint

import (
	"errors"
	"strings"
	"testing"
)

// errWriter fails every write so the renderers' error propagation is covered.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteText_NilResult(t *testing.T) {
	var sb strings.Builder
	if err := WriteText(&sb, nil); err != nil {
		t.Errorf("expected nil error for nil result, got %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("expected no output for nil result, got %q", sb.String())
	}
}

func TestWriteText_NoFindings(t *testing.T) {
	var sb strings.Builder
	if err := WriteText(&sb, &Result{Pass: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sb.String(); got != "No issues found.\n" {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestWriteText_Findings(t *testing.T) {
	result := &Result{
		Findings: []Finding{
			{Severity: Error, Rule: "nil-hpa", Message: "HPA manifest is nil or empty"},
			{Severity: Info, Rule: "single-metric", Message: "only one metric configured"},
		},
		Errors: 1,
		Infos:  1,
	}
	var sb strings.Builder
	if err := WriteText(&sb, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := sb.String()
	if !containsString(out, "[error] nil-hpa: HPA manifest is nil or empty") {
		t.Errorf("missing error finding line in %q", out)
	}
	if !containsString(out, "[info] single-metric: only one metric configured") {
		t.Errorf("missing info finding line in %q", out)
	}
	if !containsString(out, "1 error(s), 0 warning(s), 1 info(s)") {
		t.Errorf("missing summary counts in %q", out)
	}
	if !containsString(out, "Status: FAIL") {
		t.Errorf("missing FAIL status in %q", out)
	}
}

func TestWriteText_WriteError(t *testing.T) {
	result := &Result{
		Findings: []Finding{{Severity: Error, Rule: "r", Message: "m"}},
		Errors:   1,
	}
	if err := WriteText(errWriter{}, result); err == nil {
		t.Error("expected write error to propagate")
	}
}

func TestWriteSummary(t *testing.T) {
	var sb strings.Builder
	result := &Result{Errors: 1, Warnings: 2, Infos: 3, Pass: true}
	if err := WriteSummary(&sb, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := sb.String()
	if !containsString(out, "1 error(s), 2 warning(s), 3 info(s)") {
		t.Errorf("missing counts in %q", out)
	}
	if !containsString(out, "Status: PASS") {
		t.Errorf("expected PASS status in %q", out)
	}

	sb.Reset()
	if err := WriteSummary(&sb, nil); err != nil {
		t.Errorf("expected nil error for nil result, got %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("expected no output for nil result, got %q", sb.String())
	}

	if err := WriteSummary(errWriter{}, result); err == nil {
		t.Error("expected write error to propagate from counts line")
	}
}

func TestWriteDiff(t *testing.T) {
	var sb strings.Builder
	if err := WriteDiff(&sb, nil); err != nil {
		t.Errorf("expected nil error for nil result, got %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("expected no output for nil result, got %q", sb.String())
	}

	// No fixable findings: no output beyond nothing, no error.
	sb.Reset()
	if err := WriteDiff(&sb, &Result{Findings: []Finding{{Severity: Info, Rule: "r", Message: "m"}}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("expected no output without autofix findings, got %q", sb.String())
	}

	result := &Result{
		Findings: []Finding{
			{
				Severity:       Warning,
				Rule:           "behavior-scaledown",
				Message:        "no scaleDown behavior",
				Recommendation: "add scaleDown behavior",
				AutoFix: &AutoFix{
					Before:  "no scaleDown",
					After:   "stabilizationWindow 300s",
					Risk:    "low",
					Command: "kubectl patch hpa web",
					Patch:   `{"behavior":{"scaleDown":{}}}`,
				},
			},
			{Severity: Info, Rule: "single-metric", Message: "m"},
		},
	}
	sb.Reset()
	if err := WriteDiff(&sb, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		"--- [warning] behavior-scaledown ---",
		"Message: no scaleDown behavior",
		"Recommendation: add scaleDown behavior",
		"Before: no scaleDown",
		"After:  stabilizationWindow 300s",
		"Risk:   low",
		"Command: kubectl patch hpa web",
		"1 fixable issue(s) found (dry-run — no changes applied)",
	} {
		if !containsString(out, want) {
			t.Errorf("missing %q in diff output:\n%s", want, out)
		}
	}

	// A finding without a recommendation skips that line.
	noRec := &Result{Findings: []Finding{{
		Severity: Warning,
		Rule:     "r",
		Message:  "m",
		AutoFix:  &AutoFix{Command: "c"},
	}}}
	sb.Reset()
	if err := WriteDiff(&sb, noRec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(sb.String(), "Recommendation:") {
		t.Errorf("unexpected recommendation line in %q", sb.String())
	}

	if err := WriteDiff(errWriter{}, result); err == nil {
		t.Error("expected write error to propagate")
	}
}

func TestWriteCompact(t *testing.T) {
	var sb strings.Builder
	if err := WriteCompact(&sb, nil, ""); err != nil {
		t.Errorf("expected nil error for nil result, got %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("expected no output for nil result, got %q", sb.String())
	}

	result := &Result{
		Findings: []Finding{
			{Severity: Warning, Rule: "behavior-scaledown", Message: "no scaleDown behavior"},
			{Severity: Info, Rule: "single-metric", Message: "only one metric"},
		},
	}

	sb.Reset()
	if err := WriteCompact(&sb, result, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), sb.String())
	}
	if lines[0] != "[warning] behavior-scaledown: no scaleDown behavior" {
		t.Errorf("unexpected first line: %q", lines[0])
	}

	sb.Reset()
	if err := WriteCompact(&sb, result, "hpa.yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sb.String(), "hpa.yaml: [warning]") {
		t.Errorf("expected file prefix in %q", sb.String())
	}

	if err := WriteCompact(errWriter{}, result, ""); err == nil {
		t.Error("expected write error to propagate")
	}
}
