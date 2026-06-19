package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
)

// This file re-exports the shared evidence-tier enums from
// pkg/hpa/internal/confidence so existing call sites in pkg/hpa keep working.
// Sub-packages that need these types import the confidence package directly.

// Confidence represents how certain the analysis is about a diagnostic finding.
type Confidence = confidence.Confidence

const (
	// ConfidenceHigh indicates strong evidence from HPA conditions or CRD lookups.
	ConfidenceHigh = confidence.High
	// ConfidenceMedium indicates inference from metric ratios or heuristic signals.
	ConfidenceMedium = confidence.Medium
	// ConfidenceLow indicates speculative findings with limited evidence.
	ConfidenceLow = confidence.Low
)

// Classification represents the evidence tier of a diagnostic finding.
type Classification = confidence.Classification

const (
	// ClassificationObserved indicates a finding directly read from HPA status.
	ClassificationObserved = confidence.ClassificationObserved
	// ClassificationEstimated indicates a finding inferred from visible signals.
	ClassificationEstimated = confidence.ClassificationEstimated
	// ClassificationUnknown indicates information the HPA controller does not expose.
	ClassificationUnknown = confidence.ClassificationUnknown
)

// Severity represents the impact level of a diagnostic finding.
type Severity = confidence.Severity

const (
	// SeverityInfo indicates an informational finding requiring no immediate action.
	SeverityInfo = confidence.Info
	// SeverityWarning indicates a finding that warrants operator attention.
	SeverityWarning = confidence.Warning
	// SeverityError indicates a critical finding requiring intervention.
	SeverityError = confidence.Error
)
