package hpa

import "time"

// FlappingPreventionReport holds the result of flapping prevention analysis
// with what-if simulations for different stabilization window values.
type FlappingPreventionReport struct {
	// CurrentWindow is the current stabilization window in seconds.
	CurrentWindow int32 `json:"currentWindow" yaml:"currentWindow"`
	// CurrentDirectionFlips is the number of direction changes observed.
	CurrentDirectionFlips int `json:"currentDirectionFlips" yaml:"currentDirectionFlips"`
	// ObservationWindow is the time range analyzed.
	ObservationWindow string `json:"observationWindow" yaml:"observationWindow"`
	// Recommendations holds the what-if simulation results for different window values.
	Recommendations []FlappingSimulation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	// Summary is a human-readable summary of the analysis.
	Summary string `json:"summary" yaml:"summary"`
}

// FlappingSimulation holds a single what-if simulation result for a specific
// stabilization window value.
type FlappingSimulation struct {
	// WindowSeconds is the simulated stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds" yaml:"windowSeconds"`
	// EstimatedFlapReduction is the estimated percentage reduction in flapping.
	EstimatedFlapReduction float64 `json:"estimatedFlapReduction" yaml:"estimatedFlapReduction"`
	// EstimatedDirectionFlips is the estimated number of direction flips at this window.
	EstimatedDirectionFlips int `json:"estimatedDirectionFlips" yaml:"estimatedDirectionFlips"`
	// Rationale explains why this window value would reduce flapping.
	Rationale string `json:"rationale" yaml:"rationale"`
	// Patch is the JSON merge patch to apply this window value.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Confidence is the confidence level for this estimate.
	Confidence string `json:"confidence" yaml:"confidence"`
}

// FlappingDiagnosis holds the result of event-based flapping detection with
// root-cause analysis. Unlike FlappingPreventionReport which simulates window
// changes, FlappingDiagnosis identifies *why* flapping occurs and produces
// actionable recommendations with patches.
type FlappingDiagnosis struct {
	// Detected indicates whether flapping was observed.
	Detected bool `json:"detected" yaml:"detected"`
	// Severity classifies the flapping: "LOW", "MEDIUM", "HIGH", "CRITICAL".
	Severity string `json:"severity" yaml:"severity"`
	// Pattern describes the oscillation pattern (e.g. "up-down-up in 3 minutes").
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	// FlipCount is the number of direction changes observed.
	FlipCount int `json:"flipCount" yaml:"flipCount"`
	// WindowSeconds is the time span of the observed flapping.
	WindowSeconds int `json:"windowSeconds" yaml:"windowSeconds"`
	// EstimatedCauses lists the likely root causes of the flapping.
	EstimatedCauses []FlappingCause `json:"estimatedCauses,omitempty" yaml:"estimatedCauses,omitempty"`
	// Recommendations lists actionable suggestions to stop flapping.
	Recommendations []FlappingFix `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	// EventTTLLimitation warns about the Event TTL constraint.
	EventTTLLimitation string `json:"eventTtlLimitation,omitempty" yaml:"eventTtlLimitation,omitempty"`
}

// FlappingCause describes a likely root cause of HPA replica flapping.
type FlappingCause struct {
	// Type categorizes the cause: "tight-target", "short-stabilization-window",
	// "missing-scaledown-policy".
	Type string `json:"type" yaml:"type"`
	// Description explains why this cause contributes to flapping.
	Description string `json:"description" yaml:"description"`
	// Confidence is the confidence level: "high", "medium", "low".
	Confidence string `json:"confidence" yaml:"confidence"`
}

// FlappingFix describes an actionable recommendation to stop HPA flapping.
type FlappingFix struct {
	// Action describes what to do.
	Action string `json:"action" yaml:"action"`
	// Patch is an optional JSON merge patch to apply the fix.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Rationale explains why this fix helps.
	Rationale string `json:"rationale" yaml:"rationale"`
}

// AnomalyType identifies the kind of anomaly detected in health score history.
type AnomalyType string

const (
	// AnomalySuddenDegradation indicates a rapid health score drop.
	AnomalySuddenDegradation AnomalyType = "sudden-degradation"
	// AnomalyStuckState indicates the health score has not changed for an extended period.
	AnomalyStuckState AnomalyType = "stuck-state"
	// AnomalyOscillationEscalation indicates increasing oscillation in health scores.
	AnomalyOscillationEscalation AnomalyType = "oscillation-escalation"
)

// AnomalyDetection holds a single anomaly detected in health score history.
type AnomalyDetection struct {
	// Timestamp is when the anomaly was detected.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
	// Type is the anomaly type.
	Type AnomalyType `json:"type" yaml:"type"`
	// Severity is the severity: "critical", "warning", or "info".
	Severity string `json:"severity" yaml:"severity"`
	// ScoreBefore is the health score before the anomaly.
	ScoreBefore int `json:"scoreBefore" yaml:"scoreBefore"`
	// ScoreAfter is the health score after the anomaly.
	ScoreAfter int `json:"scoreAfter" yaml:"scoreAfter"`
	// Duration describes how long the anomaly condition persisted.
	Duration string `json:"duration,omitempty" yaml:"duration,omitempty"`
	// CauseEstimate is the estimated root cause of the anomaly.
	CauseEstimate string `json:"causeEstimate,omitempty" yaml:"causeEstimate,omitempty"`
	// Remediation suggests actions to address the anomaly.
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}
