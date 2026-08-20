package render

import (
	"bytes"
	"strings"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

func TestReportsEscapeUntrustedFieldsAndIncludeWarnings(t *testing.T) {
	report := hpa.StatusReport{Analysis: *hpa.NewAnalysis(hpa.FlatAnalysis{
		Name:      `<script>alert(1)</script>`,
		Namespace: "ns|next\nrow",
		Summary:   "summary\nsecond",
		Warnings:  []string{"warning <script>|next\nrow"},
	})}

	var markdown bytes.Buffer
	if err := WriteMarkdownReport(&markdown, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "## Warnings") || strings.Contains(markdown.String(), "warning <script>|next\nrow") {
		t.Fatalf("markdown warning was missing or not normalized: %q", markdown.String())
	}

	var html bytes.Buffer
	if err := WriteHTMLReport(&html, report); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.String(), "<script>alert(1)</script>") {
		t.Fatalf("raw script tag leaked into HTML: %q", html.String())
	}
	if !strings.Contains(html.String(), "<h2>Warnings</h2>") || !strings.Contains(html.String(), "&lt;script&gt;") {
		t.Fatalf("HTML warning was missing or not escaped: %q", html.String())
	}
}

func TestMarkdownEscapesExtendedSections(t *testing.T) {
	report := hpa.StatusReport{Analysis: *hpa.NewAnalysis(hpa.FlatAnalysis{
		FlappingSimulation: &simulate.SimulationResult{
			Parameter: "max<script>\nnext", RiskAssessment: "<b>risk</b>",
			Interpretation: []string{"line\n<script>alert(1)</script>"},
		},
		MetricFreshnessEntries: []hpa.MetricFreshness{{
			Name: "cpu<script>", Evidence: []string{"event\n<script>alert(2)</script>"},
			NextSteps: []string{"`break`\n<script>alert(3)</script>"},
		}},
		CapacityContext: &hpa.CapacityContext{NodeHints: []string{"hint\n<script>alert(4)</script>"}},
	})}
	var out bytes.Buffer
	if err := WriteMarkdownReport(&out, report); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "<script>") || strings.Contains(got, "\n<script>") {
		t.Fatalf("untrusted extended-section content leaked into markdown: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("expected escaped content, got %q", got)
	}
}
