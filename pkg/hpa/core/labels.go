// Package core contains dependency-light HPA data formatting contracts shared
// by the public facade and leaf renderers.
package core

// LabelProvider abstracts localized string lookup.
type LabelProvider interface {
	Get(key string) string
}

// DefaultLabels provides the canonical English fallback labels.
type DefaultLabels struct{}

var defaultLabelValues = map[string]string{
	"label_target": "Target", "label_replicas": "Replicas", "label_health": "Health score",
	"label_summary": "Summary", "label_conditions": "Conditions", "label_metrics": "Metrics",
	"label_behavior": "Behavior", "label_actions": "Recommended actions",
	"label_suggestions": "Recommended commands", "label_fix": "Fix plan",
	"label_interpretation": "Interpretation", "label_debug": "Debug", "label_keda": "KEDA",
	"label_events": "Recent events", "label_risk": "risk", "label_precondition": "precondition",
	"label_warning": "warning", "label_metrics_diagnostics": "Metrics Diagnostics",
	"label_metric_freshness": "Metrics Freshness", "label_pod_analysis": "Pod Analysis",
	"label_simulation": "Simulation", "label_capacity_context": "Capacity Context",
	"label_timeline": "Timeline", "label_metric_decision_trace": "Metric Decision Trace",
	"label_audit_findings": "Audit Findings", "label_audit_score": "Compliance Score",
	"label_audit_severity": "Severity", "label_blockers": "Scale-out blockers",
	"label_blocker_summary": "Summary", "label_blocker_interpretation": "Interpretation",
	"label_blocker_next_commands": "Next commands", "label_capacity_plan": "Capacity Plan",
	"label_metric_contract": "Metrics Contract", "label_warmup": "Warmup Analysis",
	"label_container_advisor":         "Container Resource Advisor",
	"label_behavior_advisor":          "Behavior Tuning Advisor",
	"label_flapping_prevention":       "Flapping Prevention",
	"label_structured_decision_trace": "Structured Decision Trace",
	"label_anomaly_detection":         "Anomaly Detection", "label_anomaly_type": "Type",
	"label_anomaly_severity": "Severity", "label_anomaly_cause": "Cause estimate",
	"label_anomaly_remediation": "Remediation", "label_anomaly_count": "Anomalies detected",
}

// Get returns the English label or the key itself when unknown.
func (DefaultLabels) Get(key string) string {
	if value, ok := defaultLabelValues[key]; ok {
		return value
	}
	return key
}

// DefaultLabelValues returns a copy of the canonical English label catalog.
func DefaultLabelValues() map[string]string {
	values := make(map[string]string, len(defaultLabelValues))
	for key, value := range defaultLabelValues {
		values[key] = value
	}
	return values
}
