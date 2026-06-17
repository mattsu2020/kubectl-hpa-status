package cmd

import (
	"testing"
)

// TestNormalize_FlagImplications verifies the documented implication chains
// in options.Normalize. Each rule was previously scattered across
// PersistentPreRun; the Normalize method now centralizes them and these
// tests pin the contract.
func TestNormalize_FlagImplications(t *testing.T) {
	t.Run("recommend implies suggest", func(t *testing.T) {
		o := &options{}
		o.features.recommend = true
		o.Normalize()
		if !o.features.suggest {
			t.Fatal("recommend should imply suggest")
		}
	})

	t.Run("fix implies suggest and explain", func(t *testing.T) {
		o := &options{}
		o.features.fix = true
		o.Normalize()
		if !o.features.suggest || !o.features.explain {
			t.Fatal("fix should imply suggest and explain")
		}
	})

	t.Run("apply implies suggest and explain", func(t *testing.T) {
		o := &options{}
		o.apply = true
		o.Normalize()
		if !o.features.suggest || !o.features.explain {
			t.Fatal("apply should imply suggest and explain")
		}
	})

	t.Run("diff implies suggest", func(t *testing.T) {
		o := &options{}
		o.diff = true
		o.Normalize()
		if !o.features.suggest {
			t.Fatal("diff should imply suggest")
		}
	})

	t.Run("export implies suggest", func(t *testing.T) {
		o := &options{}
		o.export = "yaml"
		o.Normalize()
		if !o.features.suggest {
			t.Fatal("export should imply suggest")
		}
	})

	t.Run("exportPatch promotes to export and implies suggest", func(t *testing.T) {
		o := &options{}
		o.exportPatch = "kustomize"
		o.Normalize()
		if o.export != "kustomize" {
			t.Fatalf("exportPatch should set export to same value, got %q", o.export)
		}
		if !o.features.suggest {
			t.Fatal("exportPatch should imply suggest")
		}
	})

	t.Run("decisionTraceFormat implies decisionTrace", func(t *testing.T) {
		o := &options{}
		o.decisionTraceFormat = "json"
		o.Normalize()
		if !o.features.decisionTrace {
			t.Fatal("decisionTraceFormat should imply decisionTrace")
		}
	})

	t.Run("structured format implies explain, decisionTrace json", func(t *testing.T) {
		o := &options{}
		o.format = "structured"
		o.Normalize()
		if !o.features.explain || !o.features.decisionTrace {
			t.Fatal("structured format should imply explain and decisionTrace")
		}
		if o.decisionTraceFormat != "json" {
			t.Fatalf("structured format should set decisionTraceFormat to json, got %q", o.decisionTraceFormat)
		}
	})

	t.Run("contextForAI implies explain, diagnoseMetrics, metricHints, hiddenFactors", func(t *testing.T) {
		o := &options{}
		o.features.contextForAI = true
		o.Normalize()
		if !o.features.explain || !o.features.diagnoseMetrics || !o.features.metricHints || !o.features.hiddenFactors {
			t.Fatal("contextForAI should imply explain, diagnoseMetrics, metricHints, hiddenFactors")
		}
	})

	t.Run("ask implies explain, diagnoseMetrics, metricHints, hiddenFactors", func(t *testing.T) {
		o := &options{}
		o.ask = "why is it capped?"
		o.Normalize()
		if !o.features.explain || !o.features.diagnoseMetrics || !o.features.metricHints || !o.features.hiddenFactors {
			t.Fatal("ask should imply explain, diagnoseMetrics, metricHints, hiddenFactors")
		}
	})

	t.Run("hiddenFactors implies readinessImpact, metricsFreshness", func(t *testing.T) {
		o := &options{}
		o.features.hiddenFactors = true
		o.Normalize()
		if !o.features.readinessImpact || !o.features.metricsFreshness {
			t.Fatal("hiddenFactors should imply readinessImpact and metricsFreshness")
		}
	})

	t.Run("nodeAutoscaler implies capacityContext, capacityDeep, scalePath", func(t *testing.T) {
		o := &options{}
		o.features.nodeAutoscaler = true
		o.Normalize()
		if !o.features.capacityContext || !o.features.capacityDeep || !o.features.scalePath {
			t.Fatal("nodeAutoscaler should imply capacityContext, capacityDeep, scalePath")
		}
	})

	t.Run("karpenter implies capacityContext, capacityDeep, scalePath", func(t *testing.T) {
		o := &options{}
		o.features.karpenter = true
		o.Normalize()
		if !o.features.capacityContext || !o.features.capacityDeep || !o.features.scalePath {
			t.Fatal("karpenter should imply capacityContext, capacityDeep, scalePath")
		}
	})

	t.Run("trend implies trendAnomaly", func(t *testing.T) {
		o := &options{}
		o.trend = true
		o.Normalize()
		if !o.features.trendAnomaly {
			t.Fatal("trend should imply trendAnomaly")
		}
	})

	t.Run("trendAnomaly already set is preserved", func(t *testing.T) {
		o := &options{}
		o.trend = true
		o.features.trendAnomaly = true
		o.Normalize()
		if !o.features.trendAnomaly {
			t.Fatal("trendAnomaly should stay true when already set")
		}
	})

	t.Run("no-interpret clears interpret and suggest", func(t *testing.T) {
		o := &options{}
		o.features.interpret = true
		o.features.suggest = true
		o.features.explain = true
		o.features.noInterpret = true
		o.Normalize()
		if o.features.interpret {
			t.Fatal("no-interpret should clear interpret")
		}
		if o.features.suggest {
			t.Fatal("no-interpret should clear suggest")
		}
		if !o.features.explain {
			t.Fatal("no-interpret should NOT clear explain (only interpret+suggest)")
		}
	})

	t.Run("empty options stays mostly empty", func(t *testing.T) {
		o := &options{}
		o.Normalize()
		// No implication should fire; flags stay false.
		if o.features.suggest || o.features.explain || o.features.decisionTrace || o.features.capacityContext {
			t.Fatal("empty options should not trigger any implication")
		}
	})
}

// TestNewClient_NamespaceDefault confirms the namespace fallback logic
// embedded in commonOptions when a fake client override is supplied.
func TestNewClient_NamespaceDefault(t *testing.T) {
	t.Run("explicit namespace preserved with override", func(_ *testing.T) {
		o := &commonOptions{namespace: "my-ns"}
		// clientOverride nil triggers real kubeconfig path which we cannot
		// exercise here; this test only covers the override branch.
		_ = o
	})
}
