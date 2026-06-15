package cmd

import (
	"testing"
)

// These tests cover pure-logic helpers in output.go that are NOT exercised by
// root_test.go / root_extra_test.go: parsePrefixedFormat (all six prefixes and
// the no-match case) and reportFormatSelection (every alias including the
// report-format pins). outputLang, normalizeSelector, normalizeTemplateType,
// outputSelection, analysisOptions, escapePrometheusLabelValue, and
// reportHasCondition already have coverage in root_extra_test.go.

func TestParsePrefixedFormat(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		wantExpr string
		wantKind string
		wantOK   bool
	}{
		{name: "jsonpath equals", format: "jsonpath={.items}", wantExpr: "{.items}", wantKind: "jsonpath", wantOK: true},
		{name: "jsonpath colon", format: "jsonpath:{.items}", wantExpr: "{.items}", wantKind: "jsonpath", wantOK: true},
		{name: "template equals maps to go-template", format: "template={{ .Name }}", wantExpr: "{{ .Name }}", wantKind: "go-template", wantOK: true},
		{name: "template colon maps to go-template", format: "template:{{ .Name }}", wantExpr: "{{ .Name }}", wantKind: "go-template", wantOK: true},
		{name: "go-template equals", format: "go-template={{ .Name }}", wantExpr: "{{ .Name }}", wantKind: "go-template", wantOK: true},
		{name: "go-template colon", format: "go-template:{{ .Name }}", wantExpr: "{{ .Name }}", wantKind: "go-template", wantOK: true},
		{name: "plain format not prefixed", format: "json", wantExpr: "", wantKind: "", wantOK: false},
		{name: "empty not prefixed", format: "", wantExpr: "", wantKind: "", wantOK: false},
		{name: "unknown prefix not matched", format: "custom={.x}", wantExpr: "", wantKind: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, kind, ok := parsePrefixedFormat(tc.format)
			if expr != tc.wantExpr || kind != tc.wantKind || ok != tc.wantOK {
				t.Fatalf("parsePrefixedFormat(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.format, expr, kind, ok, tc.wantExpr, tc.wantKind, tc.wantOK)
			}
		})
	}
}

func TestReportFormatSelection(t *testing.T) {
	tests := []struct {
		report    string
		wantFmt   string
		wantFound bool
	}{
		{"markdown", "markdown", true},
		{"md", "markdown", true},
		{"html", "html", true},
		{"incident", "incident", true},
		{"junit", "junit", true},
		{"sarif", "sarif", true},
		{"", "", false},
		{"unknown", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.report, func(t *testing.T) {
			fmtStr, ok := reportFormatSelection(tc.report)
			if fmtStr != tc.wantFmt || ok != tc.wantFound {
				t.Fatalf("reportFormatSelection(%q) = (%q, %v), want (%q, %v)",
					tc.report, fmtStr, ok, tc.wantFmt, tc.wantFound)
			}
		})
	}
}

func TestShouldColorizeExplicitModes(t *testing.T) {
	// Explicit modes are pure; only the "auto"/"" branch touches the fd.
	// A nil writer is never an *os.File, so auto returns false deterministically.
	tests := []struct {
		mode string
		want bool
	}{
		{"always", true},
		{"true", true},
		{"yes", true},
		{"ALWAYS", true}, // case-insensitive
		{"never", false},
		{"false", false},
		{"no", false},
		{"auto", false}, // nil writer → not a TTY
		{"", false},
		{"bogus", false},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			if got := shouldColorize(tc.mode, nil); got != tc.want {
				t.Fatalf("shouldColorize(%q, nil) = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}
