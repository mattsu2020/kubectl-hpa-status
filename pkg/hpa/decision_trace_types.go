package hpa

// MetricDecisionTrace holds a comprehensive per-metric analysis explaining
// which metric drove the HPA scaling decision and why.
type MetricDecisionTrace struct {
	// Metrics holds the per-metric analysis for every current metric.
	Metrics []MetricTraceEntry `json:"metrics" yaml:"metrics"`
	// Winner is the name of the metric estimated to have driven the decision.
	Winner string `json:"winner,omitempty" yaml:"winner,omitempty"`
	// WinnerConfidence is the confidence in the winner determination.
	WinnerConfidence Confidence `json:"winnerConfidence,omitempty" yaml:"winnerConfidence,omitempty"`
	// SelectPolicy is the resolved selectPolicy (Max, Min, Disabled) for the
	// direction that won (scaleUp or scaleDown).
	SelectPolicy string `json:"selectPolicy,omitempty" yaml:"selectPolicy,omitempty"`
	// StabilizationEffect describes how the stabilization window affected the decision.
	StabilizationEffect *StabilizationEffect `json:"stabilizationEffect,omitempty" yaml:"stabilizationEffect,omitempty"`
	// ToleranceEffect describes whether tolerance suppressed scaling.
	ToleranceEffect *ToleranceEffect `json:"toleranceEffect,omitempty" yaml:"toleranceEffect,omitempty"`
	// Summary is a human-readable one-line explanation of the decision.
	Summary string `json:"summary" yaml:"summary"`
}

// MetricTraceEntry holds the analysis for a single metric in the decision trace.
type MetricTraceEntry struct {
	// Name is the metric display name (e.g. "cpu", "http_requests").
	Name string `json:"name" yaml:"name"`
	// Type is the metric source type (Resource, External, Pods, Object, ContainerResource).
	Type string `json:"type" yaml:"type"`
	// Ratio is the current/target ratio. nil if unavailable.
	Ratio *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	// DistanceFromTarget is |ratio - 1.0|. 0 means at target.
	DistanceFromTarget float64 `json:"distanceFromTarget,omitempty" yaml:"distanceFromTarget,omitempty"`
	// ReplicaImpact estimates how many replicas this metric would add/remove.
	ReplicaImpact float64 `json:"replicaImpact,omitempty" yaml:"replicaImpact,omitempty"`
	// DesiredDirection indicates whether this metric wants scale-up, scale-down, or no-change.
	DesiredDirection string `json:"desiredDirection" yaml:"desiredDirection"` // "up", "down", "none"
	// WithinTolerance indicates whether the metric is within the tolerance band.
	WithinTolerance bool `json:"withinTolerance,omitempty" yaml:"withinTolerance,omitempty"`
	// Note is a human-readable explanation for this metric's state.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// DecisionTrace is a readable, step-by-step explanation of the visible HPA
// decision path. It intentionally avoids reimplementing the controller and
// marks inferred steps with confidence.
type DecisionTrace struct {
	Namespace           string                `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name                string                `json:"name,omitempty" yaml:"name,omitempty"`
	CurrentReplicas     int32                 `json:"currentReplicas" yaml:"currentReplicas"`
	VisibleDesired      int32                 `json:"visibleDesiredReplicas" yaml:"visibleDesiredReplicas"`
	EstimatedRawDesired int32                 `json:"estimatedRawDesiredReplicas,omitempty" yaml:"estimatedRawDesiredReplicas,omitempty"`
	MaxReplicas         int32                 `json:"maxReplicas" yaml:"maxReplicas"`
	MinReplicas         int32                 `json:"minReplicas" yaml:"minReplicas"`
	Metrics             []DecisionTraceMetric `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	LimitCheck          string                `json:"limitCheck,omitempty" yaml:"limitCheck,omitempty"`
	Stabilization       string                `json:"stabilization,omitempty" yaml:"stabilization,omitempty"`
	FinalInterpretation string                `json:"finalInterpretation" yaml:"finalInterpretation"`
	Confidence          Confidence            `json:"confidence" yaml:"confidence"`
	Notes               []string              `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// DecisionTraceMetric describes one metric's visible ratio and estimated
// desired replica count.
type DecisionTraceMetric struct {
	Name       string     `json:"name" yaml:"name"`
	Type       string     `json:"type" yaml:"type"`
	Current    string     `json:"current,omitempty" yaml:"current,omitempty"`
	Target     string     `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio      *float64   `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	RawDesired *int32     `json:"rawDesiredReplicas,omitempty" yaml:"rawDesiredReplicas,omitempty"`
	Formula    string     `json:"formula,omitempty" yaml:"formula,omitempty"`
	Direction  string     `json:"direction,omitempty" yaml:"direction,omitempty"`
	Confidence Confidence `json:"confidence" yaml:"confidence"`
}

// StabilizationEffect describes how the stabilization window affected the decision.
type StabilizationEffect struct {
	// WindowSeconds is the configured stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds,omitempty" yaml:"windowSeconds,omitempty"`
	// RemainingSeconds estimates how many seconds remain in the window.
	RemainingSeconds *int64 `json:"remainingSeconds,omitempty" yaml:"remainingSeconds,omitempty"`
	// SuppressedScaleDown indicates whether scale-down was suppressed by the window.
	SuppressedScaleDown bool `json:"suppressedScaleDown,omitempty" yaml:"suppressedScaleDown,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// ToleranceEffect describes whether tolerance suppressed scaling.
type ToleranceEffect struct {
	// DefaultTolerance is the Kubernetes default tolerance (0.1).
	DefaultTolerance float64 `json:"defaultTolerance" yaml:"defaultTolerance"`
	// ConfiguredTolerance is the explicitly configured tolerance, if any.
	ConfiguredTolerance *float64 `json:"configuredTolerance,omitempty" yaml:"configuredTolerance,omitempty"`
	// SuppressedMetrics lists metric names whose scaling was suppressed by tolerance.
	SuppressedMetrics []string `json:"suppressedMetrics,omitempty" yaml:"suppressedMetrics,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// StructuredDecisionTrace holds a comprehensive, schema-versioned decision
// trace that integrates per-metric analysis, tolerance/stabilization effects,
// and the winning metric determination into a single exportable document.
// Populated when --decision-trace-format is set.
type StructuredDecisionTrace struct {
	// SchemaVersion is the version of the structured trace schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// CurrentReplicas is the current replica count from HPA status.
	CurrentReplicas int32 `json:"currentReplicas" yaml:"currentReplicas"`
	// VisibleDesiredReplicas is the desired replica count from HPA status.
	VisibleDesiredReplicas int32 `json:"visibleDesiredReplicas" yaml:"visibleDesiredReplicas"`
	// EstimatedRawDesired is the estimated raw desired count before clamping.
	EstimatedRawDesired int32 `json:"estimatedRawDesiredReplicas,omitempty" yaml:"estimatedRawDesiredReplicas,omitempty"`
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32 `json:"minReplicas" yaml:"minReplicas"`
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32 `json:"maxReplicas" yaml:"maxReplicas"`
	// Metrics holds the per-metric trace entries with full evaluation.
	Metrics []StructuredMetricTrace `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	// WinnerMetric is the name of the metric that drove the scaling decision.
	WinnerMetric string `json:"winnerMetric,omitempty" yaml:"winnerMetric,omitempty"`
	// WinnerConfidence is the confidence level of the winner determination.
	WinnerConfidence Confidence `json:"winnerConfidence,omitempty" yaml:"winnerConfidence,omitempty"`
	// LimitClamp describes whether the desired was clamped by min/max.
	LimitClamp string `json:"limitClamp,omitempty" yaml:"limitClamp,omitempty"`
	// ToleranceEffect describes whether tolerance suppressed scaling.
	ToleranceEffect *ToleranceTrace `json:"toleranceEffect,omitempty" yaml:"toleranceEffect,omitempty"`
	// StabilizationEffect describes whether stabilization delayed the decision.
	StabilizationEffect *StabilizationTrace `json:"stabilizationEffect,omitempty" yaml:"stabilizationEffect,omitempty"`
	// DecisionPath lists the ordered evaluation steps that produced the result.
	DecisionPath []DecisionStep `json:"decisionPath,omitempty" yaml:"decisionPath,omitempty"`
	// Summary is a human-readable one-line explanation.
	Summary string `json:"summary" yaml:"summary"`
	// Confidence is the overall confidence of the trace.
	Confidence Confidence `json:"confidence" yaml:"confidence"`
}

// StructuredMetricTrace holds per-metric analysis within the structured decision trace.
type StructuredMetricTrace struct {
	// Name is the metric display name.
	Name string `json:"name" yaml:"name"`
	// Type is the metric source type.
	Type string `json:"type" yaml:"type"`
	// Current is the current metric value as a string.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Target is the target metric value as a string.
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
	// Ratio is the current/target ratio.
	Ratio *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	// DistanceFromTarget is |ratio - 1.0|.
	DistanceFromTarget float64 `json:"distanceFromTarget,omitempty" yaml:"distanceFromTarget,omitempty"`
	// DesiredDirection is the desired scaling direction: "up", "down", or "none".
	DesiredDirection string `json:"desiredDirection" yaml:"desiredDirection"`
	// WithinTolerance indicates whether the metric is within the tolerance band.
	WithinTolerance bool `json:"withinTolerance,omitempty" yaml:"withinTolerance,omitempty"`
	// EstimatedDesiredReplicas is the raw desired replica count from this metric alone.
	EstimatedDesiredReplicas *int32 `json:"estimatedDesiredReplicas,omitempty" yaml:"estimatedDesiredReplicas,omitempty"`
	// Formula describes the computation used to derive the desired count.
	Formula string `json:"formula,omitempty" yaml:"formula,omitempty"`
	// Confidence is the confidence level for this metric's estimation.
	Confidence Confidence `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// ToleranceTrace describes tolerance impact on the decision within the structured trace.
type ToleranceTrace struct {
	// DefaultTolerance is the Kubernetes default tolerance (0.1).
	DefaultTolerance float64 `json:"defaultTolerance" yaml:"defaultTolerance"`
	// ConfiguredTolerance is the explicitly configured tolerance, if any.
	ConfiguredTolerance *float64 `json:"configuredTolerance,omitempty" yaml:"configuredTolerance,omitempty"`
	// EffectiveTolerance is the tolerance value used for evaluation.
	EffectiveTolerance float64 `json:"effectiveTolerance" yaml:"effectiveTolerance"`
	// SuppressedMetrics lists metrics whose scaling was suppressed by tolerance.
	SuppressedMetrics []string `json:"suppressedMetrics,omitempty" yaml:"suppressedMetrics,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// StabilizationTrace describes stabilization window impact on the decision.
type StabilizationTrace struct {
	// WindowSeconds is the configured stabilization window duration.
	WindowSeconds int32 `json:"windowSeconds,omitempty" yaml:"windowSeconds,omitempty"`
	// Direction is the direction of stabilization: "scaleDown" or "scaleUp".
	Direction string `json:"direction,omitempty" yaml:"direction,omitempty"`
	// RemainingSeconds estimates seconds remaining in the window.
	RemainingSeconds *int64 `json:"remainingSeconds,omitempty" yaml:"remainingSeconds,omitempty"`
	// SuppressedDirection indicates which direction was suppressed.
	SuppressedDirection string `json:"suppressedDirection,omitempty" yaml:"suppressedDirection,omitempty"`
	// Note is a human-readable explanation.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// DecisionStep represents a single step in the HPA decision evaluation path.
type DecisionStep struct {
	// Step is the step number in the evaluation sequence.
	Step int `json:"step" yaml:"step"`
	// Description describes what was evaluated at this step.
	Description string `json:"description" yaml:"description"`
	// Result is the outcome of this evaluation step.
	Result string `json:"result" yaml:"result"`
	// Impact describes how this step affected the final decision.
	Impact string `json:"impact,omitempty" yaml:"impact,omitempty"`
	// Confidence is the confidence level for this step.
	Confidence Confidence `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}
