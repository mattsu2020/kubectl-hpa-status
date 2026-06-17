package cmdoptions

import "testing"

func TestApplyAnalysisProfile_Doctor(t *testing.T) {
	f := Features{}
	ApplyAnalysisProfile(&f, ProfileDoctor)
	if !f.Explain || !f.DiagnoseMetrics || !f.AdapterDiagnostics {
		t.Fatal("doctor profile should enable core diagnostic flags")
	}
}

func TestApplyAnalysisProfile_Quick(t *testing.T) {
	f := Features{}
	ApplyAnalysisProfile(&f, ProfileQuick)
	if !f.Interpret || !f.Explain {
		t.Fatal("quick profile should enable interpret and explain")
	}
	if f.DiagnoseMetrics {
		t.Fatal("quick profile should not enable diagnoseMetrics")
	}
}

func TestParseAnalysisProfile_Invalid(t *testing.T) {
	if _, err := ParseAnalysisProfile("unknown"); err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestApplyCommandPreset_Explain(t *testing.T) {
	root := DefaultRoot()
	local := ApplyCommandPreset(root, PresetExplain, CommandPresetOptions{StructuredFormat: true})
	if !local.Explain || !local.DecisionTrace {
		t.Fatal("explain preset should enable explain and decisionTrace")
	}
	if local.DecisionTraceFormat != "json" {
		t.Fatalf("expected json decision trace format, got %q", local.DecisionTraceFormat)
	}
	if local.Format != "structured" {
		t.Fatalf("expected structured format, got %q", local.Format)
	}
}

func TestApplyCommandPreset_DoctorDoesNotMutateOriginal(t *testing.T) {
	root := DefaultRoot()
	_ = ApplyCommandPreset(root, PresetDoctor)
	if root.Explain {
		t.Fatal("ApplyCommandPreset must not mutate the input root")
	}
}

func TestNormalize_AnalysisProfile(t *testing.T) {
	r := DefaultRoot()
	r.AnalysisProfile = ProfileIncident
	r.Normalize()
	if !r.ScaleoutBlockers || !r.DiagnoseMetrics {
		t.Fatal("incident profile should be applied during Normalize")
	}
}
