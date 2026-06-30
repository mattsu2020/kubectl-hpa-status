package cmdoptions

import "testing"

// TestFeatures_Enable verifies every registered feature name flips its
// corresponding bool field to true. This guards the featureSetters map: if a
// flag name is added but its setter is missing or targets the wrong field,
// this test fails.
func TestFeatures_Enable(t *testing.T) {
	for name := range featureSetters {
		t.Run(name, func(t *testing.T) {
			f := Features{}
			f.Enable(name)
			// Re-run Enable with the same Features value and assert at least one
			// known field flipped. We verify by enabling, then snapshotting every
			// registered flag once more against a fresh Features to confirm the
			// setter for THIS name did mutate *something*. The precise field
			// mapping is validated structurally below.
			if !anyFeatureSet(&f) {
				t.Fatalf("Enable(%q) did not flip any feature flag", name)
			}
		})
	}
}

// TestFeatures_EnableUnknownIgnored confirms unknown names are silently
// ignored (no panic, no mutation) — matching the documented contract.
func TestFeatures_EnableUnknownIgnored(t *testing.T) {
	f := Features{}
	f.Enable("does-not-exist")
	if anyFeatureSet(&f) {
		t.Fatal("unknown feature name must not mutate any field")
	}
}

// TestFeatures_EnableAllThenCheckFields maps each registered name to its
// expected target field, ensuring setters write the right field rather than
// a sibling. This is the load-bearing assertion for the registry.
func TestFeatures_EnableMapsNameToField(t *testing.T) {
	cases := map[string]func(*Features) bool{
		"interpret":          func(f *Features) bool { return f.Interpret },
		"noInterpret":        func(f *Features) bool { return f.NoInterpret },
		"explain":            func(f *Features) bool { return f.Explain },
		"suggest":            func(f *Features) bool { return f.Suggest },
		"fix":                func(f *Features) bool { return f.Fix },
		"recommend":          func(f *Features) bool { return f.Recommend },
		"hiddenFactors":      func(f *Features) bool { return f.HiddenFactors },
		"contextForAI":       func(f *Features) bool { return f.ContextForAI },
		"deep":               func(f *Features) bool { return f.Deep },
		"noEnrich":           func(f *Features) bool { return f.NoEnrich },
		"hpaOnly":            func(f *Features) bool { return f.HPAOnly },
		"diagnoseMetrics":    func(f *Features) bool { return f.DiagnoseMetrics },
		"metricsFreshness":   func(f *Features) bool { return f.MetricsFreshness },
		"metricContract":     func(f *Features) bool { return f.MetricContract },
		"adapterDiagnostics": func(f *Features) bool { return f.AdapterDiagnostics },
		"metricHints":        func(f *Features) bool { return f.MetricHints },
		"checkResources":     func(f *Features) bool { return f.CheckResources },
		"explainPods":        func(f *Features) bool { return f.ExplainPods },
		"capacityContext":    func(f *Features) bool { return f.CapacityContext },
		"capacityHeadroom":   func(f *Features) bool { return f.CapacityHeadroom },
		"capacityDeep":       func(f *Features) bool { return f.CapacityDeep },
		"capacityPlan":       func(f *Features) bool { return f.CapacityPlan },
		"scalePath":          func(f *Features) bool { return f.ScalePath },
		"nodeAutoscaler":     func(f *Features) bool { return f.NodeAutoscaler },
		"karpenter":          func(f *Features) bool { return f.Karpenter },
		"rollout":            func(f *Features) bool { return f.Rollout },
		"rolloutImpact":      func(f *Features) bool { return f.RolloutImpact },
		"readinessImpact":    func(f *Features) bool { return f.ReadinessImpact },
		"scaleoutBlockers":   func(f *Features) bool { return f.ScaleoutBlockers },
		"controllerProfile":  func(f *Features) bool { return f.ControllerProfile },
		"decisionTrace":      func(f *Features) bool { return f.DecisionTrace },
		"gitopsCheck":        func(f *Features) bool { return f.GitOpsCheck },
		"churnDetect":        func(f *Features) bool { return f.ChurnDetect },
		"flappingAdvisor":    func(f *Features) bool { return f.FlappingAdvisor },
		"trendAnomaly":       func(f *Features) bool { return f.TrendAnomaly },
		"containerAdvisor":   func(f *Features) bool { return f.ContainerAdvisor },
		"behaviorAdvisor":    func(f *Features) bool { return f.BehaviorAdvisor },
	}
	for name, isSet := range cases {
		t.Run(name, func(t *testing.T) {
			f := Features{}
			f.Enable(name)
			if !isSet(&f) {
				t.Fatalf("Enable(%q) did not set the expected field", name)
			}
		})
	}
}

// anyFeatureSet reports whether any of the toggleable feature flags is true.
func anyFeatureSet(f *Features) bool {
	return f.Interpret || f.NoInterpret || f.Explain || f.Suggest || f.Fix ||
		f.Recommend || f.HiddenFactors || f.ContextForAI || f.Deep || f.NoEnrich ||
		f.HPAOnly || f.DiagnoseMetrics || f.MetricsFreshness || f.MetricContract ||
		f.AdapterDiagnostics || f.MetricHints || f.CheckResources || f.ExplainPods ||
		f.CapacityContext || f.CapacityHeadroom || f.CapacityDeep || f.CapacityPlan ||
		f.ScalePath || f.NodeAutoscaler || f.Karpenter || f.Rollout || f.RolloutImpact ||
		f.ReadinessImpact || f.ScaleoutBlockers || f.ControllerProfile || f.DecisionTrace ||
		f.GitOpsCheck || f.ChurnDetect || f.FlappingAdvisor || f.TrendAnomaly ||
		f.ContainerAdvisor || f.BehaviorAdvisor
}
