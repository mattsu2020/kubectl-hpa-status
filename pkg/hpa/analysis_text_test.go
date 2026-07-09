package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func TestWriteStatusTextWithOptions_RendersWarnings(t *testing.T) {
	report := StatusReport{Analysis: Analyze(baseHPA(), true)}
	report.Analysis.Warnings = []string{
		"resource check unavailable: failed to read scale target resources: connection refused",
		"health trend append failed: permission denied",
	}

	var buf bytes.Buffer
	if err := WriteStatusTextWithOptions(&buf, report, StatusTextOptions{Theme: style.NewTheme(false)}); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected 'warning:' section header, got:\n%s", output)
	}
	for _, w := range report.Analysis.Warnings {
		if !strings.Contains(output, w) {
			t.Fatalf("expected warning %q in output:\n%s", w, output)
		}
	}
}

// TestWriteStatusTextWithOptions_NoWarningsSectionWhenEmpty verifies the
// warnings section is omitted entirely when there are no warnings, so the
// baseline output for healthy HPAs stays unchanged.

func TestWriteStatusTextWithOptions_NoWarningsSectionWhenEmpty(t *testing.T) {
	report := StatusReport{Analysis: Analyze(baseHPA(), true)}
	var buf bytes.Buffer
	if err := WriteStatusTextWithOptions(&buf, report, StatusTextOptions{Theme: style.NewTheme(false)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "warning:") {
		t.Fatalf("expected no warnings section for empty warnings, got:\n%s", buf.String())
	}
}
