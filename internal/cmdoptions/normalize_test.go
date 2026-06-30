package cmdoptions

import "testing"

// TestNormalize covers the implied-flag resolution in (Root).Normalize. Each
// sub-test starts from DefaultRoot, applies a single input flag, runs Normalize,
// and asserts the implied flags that should follow. These were previously only
// exercised indirectly through presets_test.go.
func TestNormalize(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(r *Root)
		assertions func(r *Root) string // returns "" on success, an error message on failure
	}{
		// --- normalizeSuggestFlags ---
		{
			name:  "Fix implies Suggest and Explain",
			setup: func(r *Root) { r.Fix = true },
			assertions: func(r *Root) string {
				if !r.Suggest {
					return "Suggest should be true when Fix is set"
				}
				if !r.Explain {
					return "Explain should be true when Fix is set"
				}
				return ""
			},
		},
		{
			name:  "Diff implies Suggest",
			setup: func(r *Root) { r.Diff = true },
			assertions: func(r *Root) string {
				if !r.Suggest {
					return "Suggest should be true when Diff is set"
				}
				return ""
			},
		},
		{
			name:  "Export implies Suggest",
			setup: func(r *Root) { r.Export = "patches" },
			assertions: func(r *Root) string {
				if !r.Suggest {
					return "Suggest should be true when Export is set"
				}
				return ""
			},
		},
		{
			name:  "Export implies Suggest",
			setup: func(r *Root) { r.Export = "patches" },
			assertions: func(r *Root) string {
				if !r.Suggest {
					return "Suggest should be true when Export is set"
				}
				return ""
			},
		},
		// --- normalizeDecisionTraceFlags ---
		{
			name:  "DecisionTraceFormat implies DecisionTrace",
			setup: func(r *Root) { r.DecisionTraceFormat = "json" },
			assertions: func(r *Root) string {
				if !r.DecisionTrace {
					return "DecisionTrace should be true when DecisionTraceFormat is set"
				}
				return ""
			},
		},
		{
			name:  "Format=structured implies Explain, DecisionTrace, json format",
			setup: func(r *Root) { r.Format = "structured" },
			assertions: func(r *Root) string {
				if !r.Explain || !r.DecisionTrace || r.DecisionTraceFormat != "json" {
					return "Format=structured should set Explain, DecisionTrace, DecisionTraceFormat=json"
				}
				return ""
			},
		},
		// --- normalizeInsightFlags ---
		{
			name:  "ContextForAI implies Explain and metrics diagnostics",
			setup: func(r *Root) { r.ContextForAI = true },
			assertions: func(r *Root) string {
				if !r.Explain || !r.DiagnoseMetrics || !r.MetricHints || !r.HiddenFactors {
					return "ContextForAI should enable Explain, DiagnoseMetrics, MetricHints, HiddenFactors"
				}
				return ""
			},
		},
		{
			name:  "Ask implies Explain and metrics diagnostics",
			setup: func(r *Root) { r.Ask = "why?" },
			assertions: func(r *Root) string {
				if !r.Explain || !r.DiagnoseMetrics {
					return "Ask should enable Explain and DiagnoseMetrics"
				}
				return ""
			},
		},
		{
			name:  "HiddenFactors implies ReadinessImpact and MetricsFreshness",
			setup: func(r *Root) { r.HiddenFactors = true },
			assertions: func(r *Root) string {
				if !r.ReadinessImpact || !r.MetricsFreshness {
					return "HiddenFactors should enable ReadinessImpact and MetricsFreshness"
				}
				return ""
			},
		},
		// --- normalizeCapacityFlags ---
		{
			name:  "NodeAutoscaler implies CapacityContext, CapacityDeep, ScalePath",
			setup: func(r *Root) { r.NodeAutoscaler = true },
			assertions: func(r *Root) string {
				if !r.CapacityContext || !r.CapacityDeep || !r.ScalePath {
					return "NodeAutoscaler should enable CapacityContext, CapacityDeep, ScalePath"
				}
				return ""
			},
		},
		{
			name:  "Karpenter implies CapacityContext, CapacityDeep, ScalePath",
			setup: func(r *Root) { r.Karpenter = true },
			assertions: func(r *Root) string {
				if !r.CapacityContext || !r.CapacityDeep || !r.ScalePath {
					return "Karpenter should enable CapacityContext, CapacityDeep, ScalePath"
				}
				return ""
			},
		},
		// --- normalizeMiscFlags ---
		{
			name:  "Trend implies TrendAnomaly",
			setup: func(r *Root) { r.Trend = true },
			assertions: func(r *Root) string {
				if !r.TrendAnomaly {
					return "Trend should enable TrendAnomaly"
				}
				return ""
			},
		},
		{
			name:  "NoInterpret disables Interpret and Suggest",
			setup: func(r *Root) { r.Interpret = true; r.Suggest = true; r.NoInterpret = true },
			assertions: func(r *Root) string {
				if r.Interpret || r.Suggest {
					return "NoInterpret should disable Interpret and Suggest"
				}
				return ""
			},
		},
		// --- applyAnalysisProfile ---
		{
			name:  "AnalysisProfile doctor is applied",
			setup: func(r *Root) { r.AnalysisProfile = ProfileDoctor },
			assertions: func(r *Root) string {
				// ProfileDoctor enables Explain, DiagnoseMetrics, MetricsFreshness, etc.
				// Verify one to confirm dispatch happened (full contents covered by presets_test.go).
				if !r.Explain {
					return "AnalysisProfile=doctor should enable Explain"
				}
				return ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := DefaultRoot()
			tc.setup(&r)
			r.Normalize()
			if msg := tc.assertions(&r); msg != "" {
				t.Fatal(msg)
			}
		})
	}
}
