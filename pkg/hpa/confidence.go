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

// Classification represents the evidence tier of a diagnostic finding,
// presented to users as Observed, Estimated, or Unknown.
type Classification string

const (
	// ClassificationObserved indicates a finding directly read from HPA status
	// fields (conditions, replicas, metrics).
	ClassificationObserved Classification = "observed"
	// ClassificationEstimated indicates a finding inferred from visible signals
	// but not directly confirmable via the API.
	ClassificationEstimated Classification = "estimated"
	// ClassificationUnknown indicates information the Kubernetes HPA controller
	// does not expose (missing-metric dampening, not-ready pod adjustments, etc.).
	ClassificationUnknown Classification = "unknown"
)

// Classify maps a Confidence level to a user-facing Classification.
func (c Confidence) Classify() Classification {
	switch c {
	case ConfidenceHigh:
		return ClassificationObserved
	case ConfidenceMedium:
		return ClassificationEstimated
	default:
		return ClassificationUnknown
	}
}

// Label returns the display string for a Classification.
func (cl Classification) Label() string {
	return string(cl)
}

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
