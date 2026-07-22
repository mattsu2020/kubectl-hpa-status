package render

import (
	"bytes"
	"strings"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestWriteHTMLReports(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteHTMLReports(&buf, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "<title>HPA Status Reports</title>") {
			t.Errorf("expected multi-report title, got:\n%s", buf.String())
		}
	})

	t.Run("multiple reports render as separate sections", func(t *testing.T) {
		var buf bytes.Buffer
		reports := []hpa.StatusReport{
			{Analysis: hpa.Analysis{Namespace: "default", Name: "web", Health: "OK"}},
			{Analysis: hpa.Analysis{Namespace: "default", Name: "api", Health: "ERROR"}},
		}
		if err := WriteHTMLReports(&buf, reports); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		if strings.Count(out, `<section class="hpa-report">`) != 2 {
			t.Errorf("expected 2 report sections, got:\n%s", out)
		}
		for _, want := range []string{"HPA Status Report: web", "HPA Status Report: api"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to mention %q, got:\n%s", want, out)
			}
		}
	})
}

func TestHTMLConditionStatus(t *testing.T) {
	tests := map[string]string{"True": "cond-true", "False": "cond-false", "Unknown": "cond-unknown"}
	for status, wantClass := range tests {
		got := htmlConditionStatus(status)
		if !strings.Contains(got, wantClass) {
			t.Errorf("htmlConditionStatus(%q) = %q, want class %q", status, got, wantClass)
		}
	}
}

func TestHTMLFreshnessBadge(t *testing.T) {
	tests := map[string]string{
		string(hpa.FreshnessOK):      "cond-true",
		string(hpa.FreshnessMissing): "cond-false",
		string(hpa.FreshnessStale):   "health-limited",
		"other":                      "cond-unknown",
	}
	for status, wantClass := range tests {
		got := htmlFreshnessBadge(status)
		if !strings.Contains(got, wantClass) {
			t.Errorf("htmlFreshnessBadge(%q) = %q, want class %q", status, got, wantClass)
		}
	}
}
