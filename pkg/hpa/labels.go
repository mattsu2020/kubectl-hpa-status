package hpa

// LabelProvider abstracts string lookup for localized labels.
// This allows pkg/hpa/ to support internationalization without
// importing internal/i18n/ (which is prohibited by Go's internal
// package boundary). The cmd/ layer wires in the concrete i18n
// implementation; callers who do not set a provider get English defaults.
type LabelProvider interface {
	Get(key string) string
}

// DefaultLabels provides English label strings. This is the zero-dependency
// fallback used when no LabelProvider is configured.
type DefaultLabels struct{}

// Get returns the English label for the given key, or the key itself if unknown.
func (DefaultLabels) Get(key string) string {
	defaults := map[string]string{
		"label_target":              "Target",
		"label_replicas":            "Replicas",
		"label_health":              "Health score",
		"label_summary":             "Summary",
		"label_conditions":          "Conditions",
		"label_metrics":             "Metrics",
		"label_behavior":            "Behavior",
		"label_actions":             "Recommended actions",
		"label_suggestions":         "Recommended commands",
		"label_fix":                 "Fix plan",
		"label_interpretation":      "Interpretation",
		"label_debug":               "Debug",
		"label_keda":                "KEDA",
		"label_events":              "Recent events",
		"label_risk":                "risk",
		"label_precondition":        "precondition",
		"label_warning":             "warning",
		"label_metrics_diagnostics": "Metrics Diagnostics",
		"label_metric_freshness":    "Metrics Freshness",
		"label_pod_analysis":        "Pod Analysis",
		"label_simulation":          "Simulation",
		"label_capacity_context":    "Capacity Context",
		"label_timeline":            "Timeline",
		"label_metric_decision_trace": "Metric Decision Trace",
		"label_audit_findings":       "Audit Findings",
		"label_audit_score":          "Compliance Score",
		"label_audit_severity":       "Severity",
		"label_blockers":             "Scale-out blockers",
		"label_blocker_summary":      "Summary",
		"label_blocker_interpretation": "Interpretation",
		"label_blocker_next_commands": "Next commands",
		"label_capacity_plan":        "Capacity Plan",
	}
	if v, ok := defaults[key]; ok {
		return v
	}
	return key
}

// resolveLabels returns a labels struct populated from the given LabelProvider.
// If the provider is nil, English defaults are used.
func resolveLabels(provider LabelProvider) labels {
	if provider == nil {
		provider = DefaultLabels{}
	}
	return labels{
		Target:              provider.Get("label_target"),
		Replicas:            provider.Get("label_replicas"),
		Health:              provider.Get("label_health"),
		Summary:             provider.Get("label_summary"),
		Conditions:          provider.Get("label_conditions"),
		Metrics:             provider.Get("label_metrics"),
		Behavior:            provider.Get("label_behavior"),
		Actions:             provider.Get("label_actions"),
		Suggestions:         provider.Get("label_suggestions"),
		Fix:                 provider.Get("label_fix"),
		Interpretation:      provider.Get("label_interpretation"),
		Debug:               provider.Get("label_debug"),
		KEDA:                provider.Get("label_keda"),
		Events:              provider.Get("label_events"),
		Risk:                provider.Get("label_risk"),
		Precondition:        provider.Get("label_precondition"),
		Warning:             provider.Get("label_warning"),
		MetricsDiagnostics:  provider.Get("label_metrics_diagnostics"),
		MetricFreshness:     provider.Get("label_metric_freshness"),
		PodAnalysis:         provider.Get("label_pod_analysis"),
		Simulation:          provider.Get("label_simulation"),
		CapacityContext:     provider.Get("label_capacity_context"),
		Timeline:            provider.Get("label_timeline"),
		MetricDecisionTrace: provider.Get("label_metric_decision_trace"),
		AuditFindings:       provider.Get("label_audit_findings"),
		AuditScore:          provider.Get("label_audit_score"),
		AuditSeverity:       provider.Get("label_audit_severity"),
		Blockers:            provider.Get("label_blockers"),
		NextCommands:        provider.Get("label_blocker_next_commands"),
		CapacityPlan:        provider.Get("label_capacity_plan"),
	}
}
