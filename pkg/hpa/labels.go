package hpa

import "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/core"

// LabelProvider abstracts string lookup for localized labels.
// This allows pkg/hpa/ to support internationalization without
// importing internal/i18n/ (which is prohibited by Go's internal
// package boundary). The cmd/ layer wires in the concrete i18n
// implementation; callers who do not set a provider get English defaults.
type LabelProvider = core.LabelProvider

// DefaultLabels provides English label strings. This is the zero-dependency
// fallback used when no LabelProvider is configured.
type DefaultLabels = core.DefaultLabels

// defaultLabelValues preserves the package-local catalog used by synchronization
// tests while core remains the canonical source.
var defaultLabelValues = core.DefaultLabelValues()

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
		MetricContract:      provider.Get("label_metric_contract"),
		Warmup:              provider.Get("label_warmup"),
		ContainerAdvisor:    provider.Get("label_container_advisor"),
		BehaviorAdvisor:     provider.Get("label_behavior_advisor"),
		FlappingPrevention:  provider.Get("label_flapping_prevention"),
	}
}
