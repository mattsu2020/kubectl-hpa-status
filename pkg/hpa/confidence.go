package hpa

// Confidence represents how certain the analysis is about a diagnostic finding.
type Confidence string

const (
	// ConfidenceHigh indicates strong evidence from HPA conditions or CRD lookups.
	ConfidenceHigh Confidence = "high"
	// ConfidenceMedium indicates inference from metric ratios or heuristic signals.
	ConfidenceMedium Confidence = "medium"
	// ConfidenceLow indicates speculative findings with limited evidence.
	ConfidenceLow Confidence = "low"
)

// Severity represents the impact level of a diagnostic finding.
type Severity string

const (
	// SeverityInfo indicates an informational finding requiring no immediate action.
	SeverityInfo Severity = "info"
	// SeverityWarning indicates a finding that warrants operator attention.
	SeverityWarning Severity = "warning"
	// SeverityError indicates a critical finding requiring intervention.
	SeverityError Severity = "error"
)
