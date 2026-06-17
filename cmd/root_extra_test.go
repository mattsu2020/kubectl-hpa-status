package cmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	corev1 "k8s.io/api/core/v1"
)

// --- eventOption tests ---

func TestEventOption_Set_True(t *testing.T) {
	var o eventOption
	if err := o.Set("true"); err != nil {
		t.Fatal(err)
	}
	if !o.enabled || o.limit != 5 {
		t.Fatalf("expected enabled=true, limit=5, got enabled=%v, limit=%d", o.enabled, o.limit)
	}
}

func TestEventOption_Set_Empty(t *testing.T) {
	var o eventOption
	if err := o.Set(""); err != nil {
		t.Fatal(err)
	}
	if !o.enabled || o.limit != 5 {
		t.Fatalf("expected enabled=true, limit=5, got enabled=%v, limit=%d", o.enabled, o.limit)
	}
}

func TestEventOption_Set_False(t *testing.T) {
	o := eventOption{enabled: true, limit: 5}
	if err := o.Set("false"); err != nil {
		t.Fatal(err)
	}
	if o.enabled {
		t.Fatal("expected enabled=false")
	}
}

func TestEventOption_Set_Number(t *testing.T) {
	var o eventOption
	if err := o.Set("10"); err != nil {
		t.Fatal(err)
	}
	if !o.enabled || o.limit != 10 {
		t.Fatalf("expected enabled=true, limit=10, got enabled=%v, limit=%d", o.enabled, o.limit)
	}
}

func TestEventOption_Set_InvalidString(t *testing.T) {
	var o eventOption
	err := o.Set("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestEventOption_Set_Zero(t *testing.T) {
	var o eventOption
	err := o.Set("0")
	if err == nil {
		t.Fatal("expected error for zero limit")
	}
}

func TestEventOption_Set_Negative(t *testing.T) {
	var o eventOption
	err := o.Set("-5")
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
}

func TestEventOption_Set_PreservesExistingLimit(t *testing.T) {
	o := eventOption{enabled: false, limit: 10}
	if err := o.Set("true"); err != nil {
		t.Fatal(err)
	}
	if o.limit != 10 {
		t.Fatalf("expected limit=10 to be preserved, got %d", o.limit)
	}
}

func TestEventOption_String(t *testing.T) {
	tests := []struct {
		enabled bool
		limit   int
		want    string
	}{
		{false, 5, "false"},
		{true, 3, "3"},
		{true, 10, "10"},
	}
	for _, tt := range tests {
		o := eventOption{enabled: tt.enabled, limit: tt.limit}
		got := o.String()
		if got != tt.want {
			t.Errorf("eventOption{enabled=%v, limit=%d}.String() = %q, want %q", tt.enabled, tt.limit, got, tt.want)
		}
	}
}

func TestEventOption_Type(t *testing.T) {
	var o eventOption
	if o.Type() != "boolOrInt" {
		t.Fatalf("expected type 'boolOrInt', got %q", o.Type())
	}
}

// --- confirmApply tests ---

func TestConfirmApply_YesResponse(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{in: strings.NewReader("y\n")},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err != nil {
		t.Fatalf("expected nil error for 'y' response, got: %v", err)
	}
}

func TestConfirmApply_YesFullWord(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{in: strings.NewReader("yes\n")},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err != nil {
		t.Fatalf("expected nil error for 'yes' response, got: %v", err)
	}
}

func TestConfirmApply_NoResponse(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{in: strings.NewReader("n\n")},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err == nil {
		t.Fatal("expected error for 'n' response")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("expected 'skipped' in error, got: %v", err)
	}
}

func TestConfirmApply_EmptyResponse(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{in: strings.NewReader("\n")},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestConfirmApply_WritesWarning(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{in: strings.NewReader("y\n")},
	}
	_ = confirmApply(&out, opts, 2, "prod", "api")
	output := out.String()
	if !strings.Contains(output, "WARNING") {
		t.Fatalf("expected WARNING in output, got: %q", output)
	}
	if !strings.Contains(output, "2 patch(es)") {
		t.Fatalf("expected patch count in output, got: %q", output)
	}
}

// --- convertPendingPodInfos tests ---

func TestConvertPendingPodInfos_Empty(t *testing.T) {
	result := convertPendingPodInfos(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertPendingPodInfos_WithData(t *testing.T) {
	details := []kube.PendingPodDetail{
		{Name: "pod-1", Unschedulable: true, Reasons: []string{"Insufficient cpu"}},
		{Name: "pod-2", Unschedulable: false, Reasons: nil},
	}
	result := convertPendingPodInfos(details)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].Name != "pod-1" {
		t.Fatalf("expected pod-1, got %s", result[0].Name)
	}
	if result[0].Phase != "Pending" {
		t.Fatalf("expected Phase=Pending, got %s", result[0].Phase)
	}
	if !result[0].Unschedulable {
		t.Fatal("expected Unschedulable=true")
	}
	if result[1].Unschedulable {
		t.Fatal("expected Unschedulable=false for pod-2")
	}
}

// --- convertToBlockerPodInfos tests (shared conversion path) ---

func TestConvertToBlockerPodInfos_Empty(t *testing.T) {
	result := convertToBlockerPodInfos(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertToBlockerPodInfos_WithData(t *testing.T) {
	details := []kube.PendingPodDetail{
		{Name: "pod-1", Unschedulable: true, Reasons: []string{"Insufficient memory"}},
	}
	result := convertToBlockerPodInfos(details)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "pod-1" {
		t.Fatalf("expected pod-1, got %s", result[0].Name)
	}
	if result[0].Phase != "Pending" {
		t.Fatalf("expected Phase=Pending, got %s", result[0].Phase)
	}
	if !result[0].Unschedulable {
		t.Fatal("expected Unschedulable=true")
	}
}

// --- convertPDBsPlain tests (capacity_plan path: no Disruption message) ---

func TestConvertPDBsPlain_NoDisruption(t *testing.T) {
	infos := []kube.PDBInfo{{Name: "web-pdb", MinAvailable: "3"}}
	result := convertPDBsPlain(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Disruption != "" {
		t.Fatalf("expected empty Disruption for plain conversion, got %q", result[0].Disruption)
	}
	if result[0].MinAvailable != "3" {
		t.Fatalf("expected MinAvailable=3, got %s", result[0].MinAvailable)
	}
}

// --- pdbDisruptionMessage tests ---

func TestPDBDisruptionMessage(t *testing.T) {
	tests := []struct {
		name string
		pdb  kube.PDBInfo
		want string
	}{
		{"minAvailable wins over both", kube.PDBInfo{MinAvailable: "3", MaxUnavailable: "1"}, "minAvailable"},
		{"maxUnavailable when no min", kube.PDBInfo{MaxUnavailable: "2"}, "maxUnavailable=2"},
		{"default when empty", kube.PDBInfo{}, "no availability constraint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pdbDisruptionMessage(tc.pdb)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("pdbDisruptionMessage(%+v) = %q, want substring %q", tc.pdb, got, tc.want)
			}
		})
	}
}

// --- convertQuotas tests ---

func TestConvertQuotas_Empty(t *testing.T) {
	result := convertQuotas(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertQuotas_WithData(t *testing.T) {
	infos := []kube.QuotaInfo{
		{Name: "compute-quota", Resource: "cpu", Used: "4", Hard: "8"},
	}
	result := convertQuotas(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "compute-quota" {
		t.Fatalf("expected compute-quota, got %s", result[0].Name)
	}
	if result[0].Resource != "cpu" {
		t.Fatalf("expected cpu, got %s", result[0].Resource)
	}
	if !strings.Contains(result[0].Message, "compute-quota") {
		t.Fatalf("expected quota name in message, got %q", result[0].Message)
	}
}

// --- convertPDBs tests ---

func TestConvertPDBs_Empty(t *testing.T) {
	result := convertPDBs(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestConvertPDBs_MinAvailable(t *testing.T) {
	infos := []kube.PDBInfo{
		{Name: "web-pdb", MinAvailable: "3"},
	}
	result := convertPDBs(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Name != "web-pdb" {
		t.Fatalf("expected web-pdb, got %s", result[0].Name)
	}
	if !strings.Contains(result[0].Disruption, "minAvailable") {
		t.Fatalf("expected minAvailable in disruption message, got %q", result[0].Disruption)
	}
}

func TestConvertPDBs_MaxUnavailable(t *testing.T) {
	infos := []kube.PDBInfo{
		{Name: "api-pdb", MaxUnavailable: "1"},
	}
	result := convertPDBs(infos)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if !strings.Contains(result[0].Disruption, "maxUnavailable") {
		t.Fatalf("expected maxUnavailable in disruption message, got %q", result[0].Disruption)
	}
}

func TestConvertPDBs_NoConstraints(t *testing.T) {
	infos := []kube.PDBInfo{
		{Name: "empty-pdb"},
	}
	result := convertPDBs(infos)
	if !strings.Contains(result[0].Disruption, "no availability constraint") {
		t.Fatalf("expected default message, got %q", result[0].Disruption)
	}
}

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

// --- shouldColorize tests ---

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

// --- parseSimulateMetricOverrides tests ---

func TestParseSimulateMetricOverrides_Valid(t *testing.T) {
	result, err := parseSimulateMetricOverrides([]string{"cpu=80%", "memory=4Gi"})
	if err != nil {
		t.Fatal(err)
	}
	if result["cpu"] != "80%" {
		t.Fatalf("expected cpu=80%%, got %q", result["cpu"])
	}
	if result["memory"] != "4Gi" {
		t.Fatalf("expected memory=4Gi, got %q", result["memory"])
	}
}

func TestParseSimulateMetricOverrides_NoEquals(t *testing.T) {
	_, err := parseSimulateMetricOverrides([]string{"cpu"})
	if err == nil {
		t.Fatal("expected error for missing equals")
	}
}

func TestParseSimulateMetricOverrides_EmptyName(t *testing.T) {
	_, err := parseSimulateMetricOverrides([]string{"=80%"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestParseSimulateMetricOverrides_Empty(t *testing.T) {
	result, err := parseSimulateMetricOverrides([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

// --- podUnschedulable tests ---

func TestPodUnschedulable_NotUnschedulable(t *testing.T) {
	pod := corev1.Pod{
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
			},
		},
	}
	if podUnschedulable(pod) {
		t.Fatal("expected false for scheduled pod")
	}
}

func TestPodUnschedulable_NoConditions(t *testing.T) {
	pod := corev1.Pod{}
	if podUnschedulable(pod) {
		t.Fatal("expected false for pod with no conditions")
	}
}

// --- options.Normalize tests ---

func TestStatusOptions_Normalize(t *testing.T) {
	tests := []struct {
		name          string
		opts          options
		wantSuggest   bool
		wantExplain   bool
		wantInterpret bool
	}{
		{
			name:        "recommend implies suggest",
			opts:        options{statusOptions: statusOptions{features: featureFlags{recommend: true}}},
			wantSuggest: true,
		},
		{
			name:        "fix implies suggest and explain",
			opts:        options{statusOptions: statusOptions{features: featureFlags{fix: true}}},
			wantSuggest: true,
			wantExplain: true,
		},
		{
			name:        "apply implies suggest and explain",
			opts:        options{commonOptions: commonOptions{apply: true}},
			wantSuggest: true,
			wantExplain: true,
		},
		{
			name:        "diff implies suggest",
			opts:        options{commonOptions: commonOptions{diff: true}},
			wantSuggest: true,
		},
		{
			name: "no-interpret clears suggest",
			opts: options{statusOptions: statusOptions{features: featureFlags{interpret: true, suggest: true, noInterpret: true}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := tt.opts
			o.Normalize()
			if o.features.suggest != tt.wantSuggest {
				t.Errorf("suggest = %v, want %v", o.features.suggest, tt.wantSuggest)
			}
			if o.features.explain != tt.wantExplain {
				t.Errorf("explain = %v, want %v", o.features.explain, tt.wantExplain)
			}
		})
	}
}

// --- analysisOptions tests ---

func TestAnalysisOptions(t *testing.T) {
	w := hpaanalysis.HealthWeights{}
	opts := analysisOptions(w, true)
	if !opts.Debug {
		t.Fatal("expected debug=true")
	}
}

// --- writePrometheus error tests ---

func TestWritePrometheusMetrics_WritesAllMetrics(t *testing.T) {
	var out bytes.Buffer
	err := writePrometheusMetrics(&out, "ns", "name", 75, 3, 5, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	output := out.String()
	for _, name := range []string{"hpa_health_score", "hpa_current_replicas", "hpa_desired_replicas", "hpa_min_replicas", "hpa_max_replicas"} {
		if !strings.Contains(output, name) {
			t.Errorf("expected metric %s in output", name)
		}
	}
}

// --- escapePrometheusLabelValue tests ---

func TestEscapePrometheusLabelValue(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"simple", "simple"},
		{`has"quote`, `has\"quote`},
		{`has\backslash`, `has\\backslash`},
		{`both\"here`, `both\\\"here`},
	}
	for _, tt := range tests {
		got := escapePrometheusLabelValue(tt.input)
		if got != tt.want {
			t.Errorf("escapePrometheusLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- reportHasCondition tests ---

func TestReportHasCondition_NoMatch(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Conditions: []hpaanalysis.Condition{{Type: "AbleToScale"}},
		},
	}
	if reportHasCondition(report, "ScalingActive") {
		t.Fatal("expected no match")
	}
}

func TestReportHasCondition_EmptyConditions(t *testing.T) {
	report := hpaanalysis.StatusReport{}
	if reportHasCondition(report, "ScalingActive") {
		t.Fatal("expected no match for empty conditions")
	}
}

// --- collectApplicablePatches tests ---

func TestCollectApplicablePatches(t *testing.T) {
	suggestions := []hpaanalysis.Suggestion{
		{Title: "max replicas", Apply: true, Patch: `{"spec":{"maxReplicas":20}}`},
		{Title: "no patch", Apply: true, Patch: ""},
		{Title: "not applicable", Apply: false, Patch: `{"spec":{"minReplicas":3}}`},
	}
	patches := collectApplicablePatches(suggestions)
	if len(patches) != 1 {
		t.Fatalf("expected 1 applicable patch, got %d", len(patches))
	}
	if patches[0].Title != "max replicas" {
		t.Fatalf("expected 'max replicas' patch, got %q", patches[0].Title)
	}
}

func TestCollectApplicablePatches_Empty(t *testing.T) {
	patches := collectApplicablePatches(nil)
	if len(patches) != 0 {
		t.Fatalf("expected 0 patches, got %d", len(patches))
	}
}

// --- dryRunResults tests ---

func TestDryRunResults(t *testing.T) {
	patches := []hpaanalysis.Suggestion{
		{Title: "increase max"},
		{Title: "increase min"},
	}
	results := dryRunResults(patches)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !strings.Contains(results[0], "increase max") {
		t.Fatalf("expected title in result, got %q", results[0])
	}
}

// --- buildCapacityContext tests ---

func TestBuildCapacityContext_NilSelector(t *testing.T) {
	// With a fake client that has no scale target resources, the selector
	// resolution will fail and return an empty result.
	hpa := testutil.BuildHPA("default", "web")
	fakeClient := testutil.NewFakeClient(hpa)
	client := &kube.Client{Interface: fakeClient, Namespace: "default"}

	result := buildCapacityContext(context.Background(), client, hpa)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
