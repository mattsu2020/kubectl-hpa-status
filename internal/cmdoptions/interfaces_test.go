package cmdoptions

import (
	"bytes"
	"testing"
)

// TestRootAccessors exercises every Root accessor method against a
// hand-populated Root so the interface contracts stay pinned. These are
// trivial getters, but they are the seam commands depend on, so a regression
// (e.g. returning the wrong field) is worth catching early.
func TestRootAccessors(t *testing.T) {
	r := &Root{}
	r.Output = "json"
	r.Template = "{{.}}"
	r.OutputTemplates = map[string]OutputTemplateConfig{"wide": {Template: "{{.}}"}}
	r.Report = "structured"
	r.Color = "always"
	r.Lang = "ja"
	r.KEDA = "on"
	r.VPA = "on"
	r.Simulate = []string{"maxReplicas=10"}
	r.SimulateMetric = []string{"cpu=80"}
	r.SimulateDuration = 300
	r.Events = EventOption{Enabled: true, Limit: 7}
	r.DecisionTrace = true
	r.DecisionTraceFormat = "json"
	r.AssumeProfile = "keda"
	r.ControllerProfileFile = "/tmp/profile.yaml"
	r.Debug = true
	r.SortBy = "name"
	r.Filter = "app=web"
	r.HealthScoreMin = 40
	r.HealthScoreMax = 80
	r.Problem = true
	r.Summary = true
	r.AllNamespaces = true
	r.Selector = "app=web"
	r.ChunkSize = 250
	in := &bytes.Buffer{}
	r.In = in

	if got := r.OutputFormat(); got != "json" {
		t.Fatalf("OutputFormat = %q, want json", got)
	}
	if got := r.OutputTemplate(); got != "{{.}}" {
		t.Fatalf("OutputTemplate = %q", got)
	}
	if got := r.NamedOutputTemplates(); len(got) != 1 || got["wide"].Template != "{{.}}" {
		t.Fatalf("NamedOutputTemplates = %+v", got)
	}
	if got := r.ReportFormat(); got != "structured" {
		t.Fatalf("ReportFormat = %q", got)
	}
	if got := r.ColorMode(); got != "always" {
		t.Fatalf("ColorMode = %q", got)
	}
	if got := r.Language(); got != "ja" {
		t.Fatalf("Language = %q", got)
	}
	if ff := r.FeatureFlags(); ff == nil {
		t.Fatal("FeatureFlags must not be nil")
	}
	if got := r.KEDAMode(); got != "on" {
		t.Fatalf("KEDAMode = %q", got)
	}
	if got := r.VPAMode(); got != "on" {
		t.Fatalf("VPAMode = %q", got)
	}
	if got := r.SimulateOverrides(); len(got) != 1 || got[0] != "maxReplicas=10" {
		t.Fatalf("SimulateOverrides = %v", got)
	}
	if got := r.SimulateMetricOverrides(); len(got) != 1 || got[0] != "cpu=80" {
		t.Fatalf("SimulateMetricOverrides = %v", got)
	}
	if got := r.SimulateDurationSeconds(); got != 300 {
		t.Fatalf("SimulateDurationSeconds = %d", got)
	}
	if enabled, limit := r.EventLimit(); !enabled || limit != 7 {
		t.Fatalf("EventLimit = (%v,%d)", enabled, limit)
	}
	if !r.DecisionTraceEnabled() {
		t.Fatal("DecisionTraceEnabled = false")
	}
	if got := r.StructuredDecisionTraceFormat(); got != "json" {
		t.Fatalf("StructuredDecisionTraceFormat = %q", got)
	}
	if got := r.AssumeControllerProfile(); got != "keda" {
		t.Fatalf("AssumeControllerProfile = %q", got)
	}
	if got := r.HPAControllerProfileFile(); got != "/tmp/profile.yaml" {
		t.Fatalf("HPAControllerProfileFile = %q", got)
	}
	if !r.DebugAnalysis() {
		t.Fatal("DebugAnalysis = false")
	}
	if got := r.ListSortBy(); got != "name" {
		t.Fatalf("ListSortBy = %q", got)
	}
	if got := r.ListFilter(); got != "app=web" {
		t.Fatalf("ListFilter = %q", got)
	}
	if lo, hi := r.HealthScoreRange(); lo != 40 || hi != 80 {
		t.Fatalf("HealthScoreRange = (%d,%d)", lo, hi)
	}
	if !r.ProblemOnly() {
		t.Fatal("ProblemOnly = false")
	}
	if !r.SummaryMode() {
		t.Fatal("SummaryMode = false")
	}
	if !r.IsAllNamespaces() {
		t.Fatal("IsAllNamespaces = false")
	}
	if got := r.LabelSelector(); got != "app=web" {
		t.Fatalf("LabelSelector = %q", got)
	}
	if got := r.ListChunkSize(); got != 250 {
		t.Fatalf("ListChunkSize = %d", got)
	}
	if r.Stdin() != in {
		t.Fatal("Stdin must return the configured reader")
	}
}

// TestRootAccessors_ZeroValue confirms accessors do not panic and return sane
// zero values when no fields are populated.
func TestRootAccessors_ZeroValue(t *testing.T) {
	r := &Root{}
	if r.GetClientOverride() != nil {
		t.Fatal("zero-value GetClientOverride must be nil")
	}
	if r.OutputFormat() != "" {
		t.Fatal("zero-value OutputFormat must be empty")
	}
	if ff := r.FeatureFlags(); ff == nil {
		t.Fatal("zero-value FeatureFlags must still return non-nil pointer")
	}
	if enabled, _ := r.EventLimit(); enabled {
		t.Fatal("zero-value EventLimit enabled must be false")
	}
}
