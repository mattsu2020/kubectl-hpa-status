package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// Output-rendering unit tests (ExitCodeError/warningExitCode, shouldColorize,
// outputLang, i18nLabels, normalizeSelector, normalizeTemplateType,
// outputSelection, writeOutput). Split out of the former root_extra_test.go
// grab-bag so each helper's tests live next to its source.

// --- ExitCodeError tests ---

func TestExitCodeError_Error(t *testing.T) {
	err := &ExitCodeError{Code: ExitWarning, Err: fmt.Errorf("test error")}
	if err.Error() != "test error" {
		t.Fatalf("expected 'test error', got %q", err.Error())
	}
}

func TestWarningExitCode_WatchMode(t *testing.T) {
	err := warningExitCode("ERROR", "web", "default", true)
	if err != nil {
		t.Fatalf("expected nil in watch mode, got %v", err)
	}
}

func TestWarningExitCode_OK(t *testing.T) {
	err := warningExitCode("OK", "web", "default", false)
	if err != nil {
		t.Fatalf("expected nil for OK health, got %v", err)
	}
}

func TestWarningExitCode_Error(t *testing.T) {
	err := warningExitCode("ERROR", "broken", "default", false)
	if err == nil {
		t.Fatal("expected error for ERROR health")
	}
	exitErr, ok := err.(*ExitCodeError)
	if !ok {
		t.Fatalf("expected *ExitCodeError, got %T", err)
	}
	if exitErr.Code != ExitWarning {
		t.Fatalf("expected code %d, got %d", ExitWarning, exitErr.Code)
	}
}

func TestWarningExitCode_Limited(t *testing.T) {
	err := warningExitCode("LIMITED", "api", "default", false)
	if err == nil {
		t.Fatal("expected error for LIMITED health")
	}
	exitErr, ok := err.(*ExitCodeError)
	if !ok {
		t.Fatalf("expected *ExitCodeError, got %T", err)
	}
	if exitErr.Code != ExitWarning {
		t.Fatalf("expected code %d, got %d", ExitWarning, exitErr.Code)
	}
}

// --- classifyError / ExitCodeForMain tests ---

func TestClassifyError_NilIsSuccess(t *testing.T) {
	if code, ok := classifyError(nil); code != ExitSuccess || !ok {
		t.Errorf("nil -> (Success, true), got (%d, %v)", code, ok)
	}
}

func TestClassifyError_ExitCodeErrorPreserved(t *testing.T) {
	wrapped := &ExitCodeError{Code: ExitWarning, Err: fmt.Errorf("limited")}
	if code, ok := classifyError(wrapped); code != ExitWarning || !ok {
		t.Errorf("ExitCodeError(Warning) -> (Warning, true), got (%d, %v)", code, ok)
	}
}

func TestClassifyError_WrappedExitCodeErrorResolvedViaErrorsAs(t *testing.T) {
	// Double-wrap to confirm errors.As traversal works.
	inner := &ExitCodeError{Code: ExitWarning, Err: fmt.Errorf("limited")}
	outer := fmt.Errorf("status failed: %w", inner)
	if code, ok := classifyError(outer); code != ExitWarning || !ok {
		t.Errorf("wrapped ExitCodeError -> (Warning, true), got (%d, %v)", code, ok)
	}
}

func TestClassifyError_ErrHPANotFoundFallsBackToExitError(t *testing.T) {
	// not-found currently maps to ExitError for backwards compatibility;
	// the sentinel is still recognised so the future ExitNotFound switch is
	// a single-line change in classifyError.
	err := fmt.Errorf("get hpa: %w", ErrHPANotFound)
	if code, ok := classifyError(err); code != ExitError || !ok {
		t.Errorf("ErrHPANotFound -> (ExitError, true), got (%d, %v)", code, ok)
	}
}

func TestClassifyError_GenericErrorIsExitError(t *testing.T) {
	code, ok := classifyError(fmt.Errorf("boom"))
	if code != ExitError || ok {
		t.Errorf("generic error -> (ExitError, false), got (%d, %v)", code, ok)
	}
}

func TestExitCodeForMain(t *testing.T) {
	if code := ExitCodeForMain(nil); code != ExitSuccess {
		t.Errorf("nil -> Success, got %d", code)
	}
	if code := ExitCodeForMain(fmt.Errorf("boom")); code != ExitError {
		t.Errorf("generic -> ExitError, got %d", code)
	}
	if code := ExitCodeForMain(&ExitCodeError{Code: ExitWarning, Err: fmt.Errorf("limited")}); code != ExitWarning {
		t.Errorf("warning -> ExitWarning, got %d", code)
	}
}

// Regression guard: ExitCodeError.Unwrap exposes the cause so errors.Is can
// match sentinels through it.
func TestExitCodeError_UnwrapExposesCause(t *testing.T) {
	wrapped := &ExitCodeError{Code: ExitError, Err: fmt.Errorf("get hpa: %w", ErrHPANotFound)}
	if !errors.Is(wrapped, ErrHPANotFound) {
		t.Error("errors.Is(ErrHPANotFound) should resolve through ExitCodeError")
	}
}

func TestShouldColorize(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"always", true},
		{"true", true},
		{"yes", true},
		{"never", false},
		{"false", false},
		{"no", false},
		{"auto", false}, // not a terminal
		{"", false},     // not a terminal
		{"invalid", false},
	}
	for _, tt := range tests {
		got := shouldColorize(tt.mode, &bytes.Buffer{})
		if got != tt.want {
			t.Errorf("shouldColorize(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

// --- outputLang tests ---

func TestOutputLang(t *testing.T) {
	tests := []struct {
		lang, output, want string
	}{
		{"ja", "", "ja"},
		{"en", "", "en"},
		{"", "ja", "ja"},
		{"", "table", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		got := outputLang(tt.lang, tt.output)
		if got != tt.want {
			t.Errorf("outputLang(%q, %q) = %q, want %q", tt.lang, tt.output, got, tt.want)
		}
	}
}

// --- i18nLabels tests ---

func TestI18nLabels_Get(_ *testing.T) {
	p := i18nLabels{lang: "ja"}
	got := p.Get("summary.steady")
	// Just verify it doesn't panic and returns something.
	_ = got
}

func TestLabelProviderForLang(t *testing.T) {
	tests := []struct {
		lang, output string
		nilResult    bool
	}{
		{"", "", true},
		{"ja", "", false},
		{"", "ja", false},
		{"en", "table", false},
	}
	for _, tt := range tests {
		p := labelProviderForLang(tt.lang, tt.output)
		if tt.nilResult && p != nil {
			t.Errorf("labelProviderForLang(%q, %q) expected nil", tt.lang, tt.output)
		}
		if !tt.nilResult && p == nil {
			t.Errorf("labelProviderForLang(%q, %q) expected non-nil", tt.lang, tt.output)
		}
	}
}

// --- normalizeSelector tests ---

func TestNormalizeSelector(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"ScalingLimited", "scalinglimited"},
		{"scaling-limited", "scalinglimited"},
		{"scaling_limited", "scalinglimited"},
		{"scaling limited", "scalinglimited"},
		{"  Scaling-Limited  ", "scalinglimited"},
	}
	for _, tt := range tests {
		got := normalizeSelector(tt.input)
		if got != tt.want {
			t.Errorf("normalizeSelector(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- normalizeTemplateType tests ---

func TestNormalizeTemplateType(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"jsonpath", "jsonpath"},
		{"go-template", "go-template"},
		{"template", "go-template"},
		{"GoTemplate", "go-template"},
		{"custom", "custom"},
	}
	for _, tt := range tests {
		got := normalizeTemplateType(tt.input)
		if got != tt.want {
			t.Errorf("normalizeTemplateType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- outputSelection tests ---

func TestOutputSelection_ReportMarkdown(t *testing.T) {
	format, tpl := outputSelection(outputConfig{report: "markdown"})
	if format != "markdown" || tpl != "" {
		t.Fatalf("expected markdown/empty, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_ReportHTML(t *testing.T) {
	format, tpl := outputSelection(outputConfig{report: "html"})
	if format != "html" || tpl != "" {
		t.Fatalf("expected html/empty, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_ReportUnknown(t *testing.T) {
	format, _ := outputSelection(outputConfig{report: "json", output: "json"})
	// report not in {markdown, md, html} -> falls through to output logic
	if format != "json" {
		t.Fatalf("expected json, got %q", format)
	}
}

func TestOutputSelection_NoTemplates(t *testing.T) {
	format, tpl := outputSelection(outputConfig{output: "json", template: "{.name}"})
	if format != "json" || tpl != "{.name}" {
		t.Fatalf("expected json/{.name}, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_EmptyOutput(t *testing.T) {
	format, tpl := outputSelection(outputConfig{output: "", outputTemplates: map[string]outputTemplateConfig{
		"custom": {Type: "go-template", Template: "hello"},
	}})
	if format != "" || tpl != "" {
		t.Fatalf("expected empty with no output, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_NamedTemplateConfig(t *testing.T) {
	format, tpl := outputSelection(outputConfig{
		output: "custom",
		outputTemplates: map[string]outputTemplateConfig{
			"custom": {Type: "go-template", Template: "hello"},
		},
	})
	if format != "go-template" || tpl != "hello" {
		t.Fatalf("expected go-template/hello, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_NamedTemplateConfigEmptyType(t *testing.T) {
	format, tpl := outputSelection(outputConfig{
		output: "custom",
		outputTemplates: map[string]outputTemplateConfig{
			"custom": {Template: "hello"},
		},
	})
	if format != "go-template" || tpl != "hello" {
		t.Fatalf("expected go-template/hello, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_JsonpathPrefix(t *testing.T) {
	format, tpl := outputSelection(outputConfig{
		output: "jsonpath:summary",
		outputTemplates: map[string]outputTemplateConfig{
			"summary": {Template: "{.analysis.summary}"},
		},
	})
	if format != "jsonpath" || tpl != "{.analysis.summary}" {
		t.Fatalf("expected jsonpath/{.analysis.summary}, got %q/%q", format, tpl)
	}
}

func TestOutputSelection_TemplatePrefix(t *testing.T) {
	format, tpl := outputSelection(outputConfig{
		output: "template:detail",
		outputTemplates: map[string]outputTemplateConfig{
			"detail": {Type: "template", Template: "{{ .Name }}"},
		},
	})
	if format != "go-template" || tpl != "{{ .Name }}" {
		t.Fatalf("expected go-template/{{ .Name }}, got %q/%q", format, tpl)
	}
}

// --- writeOutput tests ---

func TestWriteOutput_Table(t *testing.T) {
	var out bytes.Buffer
	err := writeOutput(&out, "table", "", "test", func() error {
		_, err := out.WriteString("table output")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "table output") {
		t.Fatalf("expected table output, got %q", out.String())
	}
}

func TestWriteOutput_Wide(t *testing.T) {
	var out bytes.Buffer
	err := writeOutput(&out, "wide", "", nil, func() error {
		_, err := out.WriteString("wide output")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteOutput_JA(t *testing.T) {
	var out bytes.Buffer
	err := writeOutput(&out, "ja", "", nil, func() error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteOutput_JSON(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Name: "web"},
	}
	var out bytes.Buffer
	err := writeOutput(&out, "json", "", report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name"`) {
		t.Fatalf("expected JSON output with name field, got %q", out.String())
	}
}

func TestWriteOutput_YAML(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Name: "web"},
	}
	var out bytes.Buffer
	err := writeOutput(&out, "yaml", "", report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "name: web") {
		t.Fatalf("expected YAML output with name: web, got %q", out.String())
	}
}

func TestWriteOutput_JsonpathPrefix(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Name: "web", Summary: "OK"},
	}
	var out bytes.Buffer
	err := writeOutput(&out, "jsonpath={.analysis.name}", "", report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "web" {
		t.Fatalf("expected 'web', got %q", out.String())
	}
}

func TestWriteOutput_TemplatePrefix(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Name: "web"},
	}
	var out bytes.Buffer
	err := writeOutput(&out, "template={{ .Analysis.Name }}", "", report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "web" {
		t.Fatalf("expected 'web', got %q", out.String())
	}
}

func TestWriteOutput_Unsupported(t *testing.T) {
	var out bytes.Buffer
	err := writeOutput(&out, "xml", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestWriteOutput_GoTemplateEquals(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{Name: "web"},
	}
	var out bytes.Buffer
	err := writeOutput(&out, "go-template={{ .Analysis.Name }}", "", report, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "web" {
		t.Fatalf("expected 'web', got %q", out.String())
	}
}
