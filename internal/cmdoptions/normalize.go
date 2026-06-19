package cmdoptions

import (
	"fmt"
	"os"
	"sync"
)

// deprecationWarned tracks whether each deprecated alias has already emitted its
// stderr notice in this process, so we warn once per alias rather than on every
// normalize call.
var deprecationWarned sync.Map

func warnDeprecatedOnce(flagName, reason string) {
	if _, loaded := deprecationWarned.LoadOrStore(flagName, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: --%s is deprecated: %s\n", flagName, reason)
}

// Normalize resolves implied flag settings for the status workflow.
func (r *Root) Normalize() {
	r.applyAnalysisProfile()
	r.normalizeSuggestFlags()
	r.normalizeDecisionTraceFlags()
	r.normalizeInsightFlags()
	r.normalizeCapacityFlags()
	r.normalizeMiscFlags()
}

func (r *Root) applyAnalysisProfile() {
	if r.AnalysisProfile == "" {
		return
	}
	ApplyAnalysisProfile(&r.Features, r.AnalysisProfile)
}

func (r *Root) normalizeSuggestFlags() {
	if r.Recommend {
		warnDeprecatedOnce("recommend", "use --suggest instead; scheduled for removal in v2.0")
		r.Suggest = true
	}
	if r.Fix || r.Apply {
		r.Suggest = true
		r.Explain = true
	}
	if r.Diff {
		r.Suggest = true
	}
	if r.Export != "" {
		r.Suggest = true
	}
	if r.ExportPatch != "" {
		warnDeprecatedOnce("export-patch", "use --export instead; scheduled for removal in v2.0")
		r.Export = r.ExportPatch
		r.Suggest = true
	}
}

func (r *Root) normalizeDecisionTraceFlags() {
	if r.DecisionTraceFormat != "" {
		r.DecisionTrace = true
	}
	if r.Format == "structured" {
		r.Explain = true
		r.DecisionTrace = true
		r.DecisionTraceFormat = "json"
	}
}

func (r *Root) normalizeInsightFlags() {
	if r.ContextForAI || r.Ask != "" {
		r.Explain = true
		r.DiagnoseMetrics = true
		r.MetricHints = true
		r.HiddenFactors = true
	}
	if r.HiddenFactors {
		r.ReadinessImpact = true
		r.MetricsFreshness = true
	}
}

func (r *Root) normalizeCapacityFlags() {
	if r.NodeAutoscaler || r.Karpenter {
		r.CapacityContext = true
		r.CapacityDeep = true
		r.ScalePath = true
	}
}

func (r *Root) normalizeMiscFlags() {
	if r.Trend && !r.TrendAnomaly {
		r.TrendAnomaly = true
	}
	if r.NoInterpret {
		r.Interpret = false
		r.Suggest = false
	}
}
