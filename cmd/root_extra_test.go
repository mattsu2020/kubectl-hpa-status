package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	corev1 "k8s.io/api/core/v1"
)

// Remaining unit tests from the former root_extra_test.go grab-bag that did
// not cluster with a single helper source file: EventOption, confirmApply,
// parseSimulateMetricOverrides, podUnschedulable, options.Normalize,
// analysisOptions, writePrometheus, escapePrometheusLabelValue,
// reportHasCondition, collectApplicablePatches, buildCapacityContext.

// --- EventOption tests ---

func TestEventOption_Set_True(t *testing.T) {
	var o EventOption
	if err := o.Set("true"); err != nil {
		t.Fatal(err)
	}
	if !o.Enabled || o.Limit != 5 {
		t.Fatalf("expected enabled=true, limit=5, got enabled=%v, limit=%d", o.Enabled, o.Limit)
	}
}

func TestEventOption_Set_Empty(t *testing.T) {
	var o EventOption
	if err := o.Set(""); err != nil {
		t.Fatal(err)
	}
	if !o.Enabled || o.Limit != 5 {
		t.Fatalf("expected enabled=true, limit=5, got enabled=%v, limit=%d", o.Enabled, o.Limit)
	}
}

func TestEventOption_Set_False(t *testing.T) {
	o := EventOption{Enabled: true, Limit: 5}
	if err := o.Set("false"); err != nil {
		t.Fatal(err)
	}
	if o.Enabled {
		t.Fatal("expected enabled=false")
	}
}

func TestEventOption_Set_Number(t *testing.T) {
	var o EventOption
	if err := o.Set("10"); err != nil {
		t.Fatal(err)
	}
	if !o.Enabled || o.Limit != 10 {
		t.Fatalf("expected enabled=true, limit=10, got enabled=%v, limit=%d", o.Enabled, o.Limit)
	}
}

func TestEventOption_Set_InvalidString(t *testing.T) {
	var o EventOption
	err := o.Set("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestEventOption_Set_Zero(t *testing.T) {
	var o EventOption
	err := o.Set("0")
	if err == nil {
		t.Fatal("expected error for zero limit")
	}
}

func TestEventOption_Set_Negative(t *testing.T) {
	var o EventOption
	err := o.Set("-5")
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
}

func TestEventOption_Set_PreservesExistingLimit(t *testing.T) {
	o := EventOption{Enabled: false, Limit: 10}
	if err := o.Set("true"); err != nil {
		t.Fatal(err)
	}
	if o.Limit != 10 {
		t.Fatalf("expected limit=10 to be preserved, got %d", o.Limit)
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
		o := EventOption{Enabled: tt.enabled, Limit: tt.limit}
		got := o.String()
		if got != tt.want {
			t.Errorf("EventOption{enabled=%v, limit=%d}.String() = %q, want %q", tt.enabled, tt.limit, got, tt.want)
		}
	}
}

func TestEventOption_Type(t *testing.T) {
	var o EventOption
	if o.Type() != "boolOrInt" {
		t.Fatalf("expected type 'boolOrInt', got %q", o.Type())
	}
}

// --- confirmApply tests ---

func TestConfirmApply_YesResponse(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		Common: commonOptions{
			In: strings.NewReader("y\n"),
		},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err != nil {
		t.Fatalf("expected nil error for 'y' response, got: %v", err)
	}
}

func TestConfirmApply_YesFullWord(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		Common: commonOptions{
			In: strings.NewReader("yes\n"),
		},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err != nil {
		t.Fatalf("expected nil error for 'yes' response, got: %v", err)
	}
}

func TestConfirmApply_NoResponse(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		Common: commonOptions{
			In: strings.NewReader("n\n"),
		},
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
		Common: commonOptions{
			In: strings.NewReader("\n"),
		},
	}
	err := confirmApply(&out, opts, 1, "default", "web")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestConfirmApply_WritesWarning(t *testing.T) {
	var out bytes.Buffer
	opts := &options{
		Common: commonOptions{
			In: strings.NewReader("y\n"),
		},
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
			name: "fix implies suggest and explain",
			opts: options{
				Status: statusOptions{
					Features: featuresOptions{
						Fix: true,
					},
				},
			},
			wantSuggest: true,
			wantExplain: true,
		},
		{
			name: "apply implies suggest and explain",
			opts: options{
				Common: commonOptions{
					Apply: true,
				},
			},
			wantSuggest: true,
			wantExplain: true,
		},
		{
			name: "diff implies suggest",
			opts: options{
				Common: commonOptions{
					Diff: true,
				},
			},
			wantSuggest: true,
		},
		{
			name: "no-interpret clears suggest",
			opts: options{
				Status: statusOptions{
					Features: featuresOptions{
						Interpret:   true,
						Suggest:     true,
						NoInterpret: true,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := tt.opts
			o.Normalize()
			if o.Suggest != tt.wantSuggest {
				t.Errorf("suggest = %v, want %v", o.Suggest, tt.wantSuggest)
			}
			if o.Explain != tt.wantExplain {
				t.Errorf("explain = %v, want %v", o.Explain, tt.wantExplain)
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
