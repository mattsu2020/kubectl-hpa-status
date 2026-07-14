package render

import (
	"bytes"
	"strings"
	"testing"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestReportsEscapeUntrustedFieldsAndIncludeWarnings(t *testing.T) {
	report := hpa.StatusReport{Analysis: hpa.Analysis{
		Name:      `<script>alert(1)</script>`,
		Namespace: "ns|next\nrow",
		Summary:   "summary\nsecond",
		Warnings:  []string{"warning <script>|next\nrow"},
	}}

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
