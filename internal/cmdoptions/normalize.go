package cmdoptions

// Normalize resolves implied flag settings for the status workflow.
func (r *Root) Normalize() {
	r.applyAnalysisProfile()
	r.normalizeSuggestFlags()
	r.normalizeDecisionTraceFlags()
	r.normalizeInsightFlags()
	r.normalizeCapacityFlags()
	r.normalizeDepthFlags()
	r.normalizeMiscFlags()
}

func (r *Root) applyAnalysisProfile() {
	if r.AnalysisProfile == "" {
		return
	}
	ApplyAnalysisProfile(&r.Features, r.AnalysisProfile)
}

func (r *Root) normalizeSuggestFlags() {
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
	// --deep is a one-flag depth tier that enables the capacity/rollout/
	// readiness/adapter-diagnostics enrichers together. It is additive, so a
	// user can still combine it with individual flags. It also pulls in the
	// scale-target pod observations (targetReplicaObservations) that the
	// capacity and rollout enrichers consume. The expansion is shared with
	// --analysis-profile deep via applyDeepFeatures so the two entry points
	// cannot drift.
	if r.Deep {
		applyDeepFeatures(&r.Features)
	}
}

// normalizeDepthFlags resolves the --no-enrich / --hpa-only RBAC-light tier.
// --hpa-only is an alias for --no-enrich; either disables every enricher so
// status shows only the HPA object itself. This keeps status usable in
// audited or restricted-RBAC environments where Pod/Deployment reads are not
// available. NoEnrich short-circuits buildStatusEnrichers in status.go.
func (r *Root) normalizeDepthFlags() {
	if r.HPAOnly {
		r.NoEnrich = true
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
