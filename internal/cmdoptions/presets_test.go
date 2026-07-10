package cmdoptions

import (
	"reflect"
	"strings"
	"testing"
)

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

func TestParseAnalysisProfile_ValidValues(t *testing.T) {
	for _, valid := range ValidAnalysisProfiles() {
		got, err := ParseAnalysisProfile("  " + strings.ToUpper(valid) + " ")
		if err != nil {
			t.Errorf("ParseAnalysisProfile(%q): %v", valid, err)
		}
		if string(got) != valid {
			t.Errorf("ParseAnalysisProfile(%q) = %q, want normalized %q", valid, got, valid)
		}
	}
	if got, err := ParseAnalysisProfile(""); err != nil || got != "" {
		t.Errorf("empty profile should parse to empty, got (%q, %v)", got, err)
	}
}

func TestAnalysisProfileFlagValue(t *testing.T) {
	var p AnalysisProfile
	if err := p.Set("Doctor"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if p.String() != "doctor" {
		t.Errorf("String() = %q, want doctor", p.String())
	}
	if p.Type() != "analysisProfile" {
		t.Errorf("Type() = %q", p.Type())
	}
	if err := p.Set("bogus"); err == nil {
		t.Error("Set with invalid profile should error")
	}
}

func TestApplyAnalysisProfile_AllProfiles(t *testing.T) {
	// Representative flags per profile; the full bundles are asserted loosely
	// via the non-zero check so adding a flag to a bundle does not break this
	// test, while breaking a bundle's core switch still fails loudly.
	representative := map[AnalysisProfile]func(Features) bool{
		ProfileQuick:     func(f Features) bool { return f.Interpret && f.Explain },
		ProfileStandard:  func(f Features) bool { return f.Explain },
		ProfileIncident:  func(f Features) bool { return f.ScaleoutBlockers && f.ControllerProfile },
		ProfileDoctor:    func(f Features) bool { return f.DiagnoseMetrics && f.ChurnDetect },
		ProfileMetrics:   func(f Features) bool { return f.DiagnoseMetrics && f.MetricContract },
		ProfileCapacity:  func(f Features) bool { return f.CapacityHeadroom && f.ScaleoutBlockers },
		ProfileReadiness: func(f Features) bool { return f.ReadinessImpact && f.RolloutImpact },
		ProfileDeep:      func(f Features) bool { return f.Deep && f.AdapterDiagnostics },
	}
	for profile, check := range representative {
		var f Features
		ApplyAnalysisProfile(&f, profile)
		if !check(f) {
			t.Errorf("profile %s did not enable its representative flags: %+v", profile, f)
		}
	}

	var untouched Features
	ApplyAnalysisProfile(&untouched, AnalysisProfile("unknown"))
	if !reflect.DeepEqual(untouched, Features{}) {
		t.Errorf("unknown profile must not change features: %+v", untouched)
	}
}

func TestApplyCommandPreset_AllPresets(t *testing.T) {
	for preset := range presetAppliers {
		t.Run(string(preset), func(t *testing.T) {
			root := DefaultRoot()
			before := root.Copy()

			local := ApplyCommandPreset(root, preset)

			if reflect.DeepEqual(local, before) {
				t.Errorf("preset %s changed nothing on the returned copy", preset)
			}
			// The input root must never be mutated: commands share it.
			if !reflect.DeepEqual(root, before) {
				t.Errorf("preset %s mutated the shared root options", preset)
			}
		})
	}

	t.Run("unknown preset is a no-op copy", func(t *testing.T) {
		root := DefaultRoot()
		local := ApplyCommandPreset(root, CommandPreset("unknown"))
		if !reflect.DeepEqual(local, root) {
			t.Error("unknown preset should return an unchanged copy")
		}
	})
}

func TestApplyCommandPreset_Overrides(t *testing.T) {
	t.Run("explain decision trace format", func(t *testing.T) {
		local := ApplyCommandPreset(DefaultRoot(), PresetExplain, CommandPresetOptions{DecisionTraceFormat: "text"})
		if local.DecisionTraceFormat != "text" {
			t.Errorf("override format not applied, got %q", local.DecisionTraceFormat)
		}
	})

	t.Run("blockers events override", func(t *testing.T) {
		events := EventOption{Enabled: true, Limit: 3}
		local := ApplyCommandPreset(DefaultRoot(), PresetBlockers, CommandPresetOptions{Events: &events})
		if local.Events != events {
			t.Errorf("events override not applied: %+v", local.Events)
		}
		defaulted := ApplyCommandPreset(DefaultRoot(), PresetBlockers)
		if defaulted.Events.Limit != 10 {
			t.Errorf("blockers default event limit should be 10, got %d", defaulted.Events.Limit)
		}
	})

	t.Run("bundle KEDA/VPA override", func(t *testing.T) {
		local := ApplyCommandPreset(DefaultRoot(), PresetBundle, CommandPresetOptions{KEDA: "off", VPA: "auto"})
		if local.KEDA != "off" || local.VPA != "auto" {
			t.Errorf("KEDA/VPA overrides not applied: keda=%q vpa=%q", local.KEDA, local.VPA)
		}
		defaulted := ApplyCommandPreset(DefaultRoot(), PresetBundle)
		if defaulted.KEDA != "on" || defaulted.VPA != "on" {
			t.Errorf("bundle should force keda/vpa on, got keda=%q vpa=%q", defaulted.KEDA, defaulted.VPA)
		}
	})
}
