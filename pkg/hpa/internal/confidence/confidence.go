// Package confidence defines the shared evidence-tier enums (Confidence,
// Classification, Severity) used across the pkg/hpa analysis domains. These
// were lifted out of pkg/hpa/confidence.go so leaf sub-packages (warmup,
// blocker, etc.) can use them without reaching back into the analysis core.
package confidence

// Confidence represents how certain the analysis is about a diagnostic finding.
type Confidence string

const (
	// High indicates strong evidence from HPA conditions or CRD lookups.
	High Confidence = "high"
	// Medium indicates inference from metric ratios or heuristic signals.
	Medium Confidence = "medium"
	// Low indicates speculative findings with limited evidence.
	Low Confidence = "low"
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
	case High:
		return ClassificationObserved
	case Medium:
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
	// Info indicates an informational finding requiring no immediate action.
	Info Severity = "info"
	// Warning indicates a finding that warrants operator attention.
	Warning Severity = "warning"
	// Error indicates a critical finding requiring intervention.
	Error Severity = "error"
)
