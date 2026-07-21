package gitops

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func TestWriteReviewText(t *testing.T) {
	t.Run("nil review", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteReviewText(&buf, nil, style.Theme{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("expected empty output for nil review, got %q", buf.String())
		}
	})

	t.Run("no files", func(t *testing.T) {
		var buf bytes.Buffer
		review := &Review{Summary: "no files reviewed", RiskLevel: "none"}
		if err := WriteReviewText(&buf, review, style.Theme{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "No HPA manifests found") {
			t.Errorf("expected no-manifests message, got %q", buf.String())
		}
	})

	t.Run("file with no issues", func(t *testing.T) {
		var buf bytes.Buffer
		review := &Review{
			Files:     []ReviewFile{{Path: "hpa.yaml"}},
			Summary:   "clean",
			RiskLevel: "none",
		}
		if err := WriteReviewText(&buf, review, style.Theme{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "hpa.yaml: no issues") {
			t.Errorf("expected no-issues line, got %q", buf.String())
		}
	})

	t.Run("file with findings and recommendation", func(t *testing.T) {
		var buf bytes.Buffer
		review := &Review{
			Files: []ReviewFile{{
				Path:    "hpa.yaml",
				HPAName: "web",
				Findings: []ReviewFinding{
					{Severity: "high", Category: "maxReplicas", Message: "maxReplicas decreased", Detail: "from 20 to 5"},
					{Severity: "medium", Category: "stabilization", Message: "stabilization window removed"},
					{Severity: "low", Category: "target", Message: "target unchanged"},
					{Severity: "", Category: "metric", Message: "unknown severity finding"},
				},
			}},
			Summary:        "4 findings across 1 file",
			RiskLevel:      "high",
			Recommendation: "Review the maxReplicas decrease carefully before merging this change.",
		}
		if err := WriteReviewText(&buf, review, style.Theme{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		for _, want := range []string{
			"hpa.yaml (web):", "[HIGH]", "[MED]", "[LOW]", "[INFO]",
			"from 20 to 5", "Summary: 4 findings across 1 file", "Risk level: high",
			"Recommendation:",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})
}

func TestReviewSeverityBadge(t *testing.T) {
	theme := style.Theme{}
	tests := map[string]string{"high": "[HIGH]", "medium": "[MED]", "low": "[LOW]", "unknown": "[INFO]"}
	for severity, want := range tests {
		if got := reviewSeverityBadge(severity, theme); !strings.Contains(got, want) {
			t.Errorf("reviewSeverityBadge(%q) = %q, want to contain %q", severity, got, want)
		}
	}
}

func TestReviewRiskLabel(t *testing.T) {
	theme := style.Theme{}
	tests := []string{"high", "medium", "low", "none"}
	for _, level := range tests {
		if got := reviewRiskLabel(level, theme); !strings.Contains(got, level) {
			t.Errorf("reviewRiskLabel(%q) = %q, want to contain %q", level, got, level)
		}
	}
}
