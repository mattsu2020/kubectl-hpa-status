package cmd

import "testing"

func TestNormalize_FlagImplications(t *testing.T) {
	t.Run("fix implies suggest and explain", func(t *testing.T) {
		o := &options{}
		o.Fix = true
		o.Normalize()
		if !o.Suggest || !o.Explain {
			t.Fatal("fix should imply suggest and explain")
		}
	})

	t.Run("apply implies suggest and explain", func(t *testing.T) {
		o := &options{}
		o.Apply = true
		o.Normalize()
		if !o.Suggest || !o.Explain {
			t.Fatal("apply should imply suggest and explain")
		}
	})

	t.Run("diff implies suggest", func(t *testing.T) {
		o := &options{}
		o.Diff = true
		o.Normalize()
		if !o.Suggest {
			t.Fatal("diff should imply suggest")
		}
	})

	t.Run("export implies suggest", func(t *testing.T) {
		o := &options{}
		o.Export = "yaml"
		o.Normalize()
		if !o.Suggest {
			t.Fatal("export should imply suggest")
		}
	})

	t.Run("decisionTraceFormat implies decisionTrace", func(t *testing.T) {
		o := &options{}
		o.DecisionTraceFormat = "json"
		o.Normalize()
		if !o.DecisionTrace {
			t.Fatal("decisionTraceFormat should imply decisionTrace")
		}
	})

	t.Run("structured format implies explain, decisionTrace json", func(t *testing.T) {
		o := &options{}
		o.Format = "structured"
		o.Normalize()
		if !o.Explain || !o.DecisionTrace {
			t.Fatal("structured format should imply explain and decisionTrace")
		}
		if o.DecisionTraceFormat != "json" {
			t.Fatalf("structured format should set decisionTraceFormat to json, got %q", o.DecisionTraceFormat)
		}
	})

	t.Run("contextForAI implies explain, diagnoseMetrics, metricHints, hiddenFactors", func(t *testing.T) {
		o := &options{}
		o.ContextForAI = true
		o.Normalize()
		if !o.Explain || !o.DiagnoseMetrics || !o.MetricHints || !o.HiddenFactors {
			t.Fatal("contextForAI should imply explain, diagnoseMetrics, metricHints, hiddenFactors")
		}
	})

	t.Run("ask implies explain, diagnoseMetrics, metricHints, hiddenFactors", func(t *testing.T) {
		o := &options{}
		o.Ask = "why is it capped?"
		o.Normalize()
		if !o.Explain || !o.DiagnoseMetrics || !o.MetricHints || !o.HiddenFactors {
			t.Fatal("ask should imply explain, diagnoseMetrics, metricHints, hiddenFactors")
		}
	})

	t.Run("hiddenFactors implies readinessImpact, metricsFreshness", func(t *testing.T) {
		o := &options{}
		o.HiddenFactors = true
		o.Normalize()
		if !o.ReadinessImpact || !o.MetricsFreshness {
			t.Fatal("hiddenFactors should imply readinessImpact and metricsFreshness")
		}
	})

	t.Run("nodeAutoscaler implies capacityContext, capacityDeep, scalePath", func(t *testing.T) {
		o := &options{}
		o.NodeAutoscaler = true
		o.Normalize()
		if !o.CapacityContext || !o.CapacityDeep || !o.ScalePath {
			t.Fatal("nodeAutoscaler should imply capacityContext, capacityDeep, scalePath")
		}
	})

	t.Run("karpenter implies capacityContext, capacityDeep, scalePath", func(t *testing.T) {
		o := &options{}
		o.Karpenter = true
		o.Normalize()
		if !o.CapacityContext || !o.CapacityDeep || !o.ScalePath {
			t.Fatal("karpenter should imply capacityContext, capacityDeep, scalePath")
		}
	})

	t.Run("trend implies trendAnomaly", func(t *testing.T) {
		o := &options{}
		o.Trend = true
		o.Normalize()
		if !o.TrendAnomaly {
			t.Fatal("trend should imply trendAnomaly")
		}
	})

	t.Run("trendAnomaly already set is preserved", func(t *testing.T) {
		o := &options{}
		o.Trend = true
		o.TrendAnomaly = true
		o.Normalize()
		if !o.TrendAnomaly {
			t.Fatal("trendAnomaly should stay true when already set")
		}
	})

	t.Run("no-interpret clears interpret and suggest", func(t *testing.T) {
		o := &options{}
		o.Interpret = true
		o.Suggest = true
		o.Explain = true
		o.NoInterpret = true
		o.Normalize()
		if o.Interpret {
			t.Fatal("no-interpret should clear interpret")
		}
		if o.Suggest {
			t.Fatal("no-interpret should clear suggest")
		}
		if !o.Explain {
			t.Fatal("no-interpret should NOT clear explain (only interpret+suggest)")
		}
	})

	t.Run("analysis-profile incident enables incident bundle", func(t *testing.T) {
		o := &options{}
		o.AnalysisProfile = "incident"
		o.Normalize()
		if !o.ScaleoutBlockers || !o.DiagnoseMetrics {
			t.Fatal("incident profile should enable incident-oriented flags")
		}
	})

	t.Run("empty options stays mostly empty", func(t *testing.T) {
		o := &options{}
		o.Normalize()
		if o.Suggest || o.Explain || o.DecisionTrace || o.CapacityContext {
			t.Fatal("empty options should not trigger any implication")
		}
	})
}
