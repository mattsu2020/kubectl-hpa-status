package hpa

// SimulationResult holds the before/after comparison of an HPA simulation.
type SimulationResult struct {
	Parameter            string             `json:"parameter" yaml:"parameter"`
	OriginalValue        string             `json:"originalValue" yaml:"originalValue"`
	SimulatedValue       string             `json:"simulatedValue" yaml:"simulatedValue"`
	Before               SimulationState    `json:"before" yaml:"before"`
	After                SimulationState    `json:"after" yaml:"after"`
	Confidence           string             `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	RiskAssessment       string             `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
	Interpretation       []string           `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	MetricSimulations    []MetricSimulation `json:"metricSimulations,omitempty" yaml:"metricSimulations,omitempty"`
	TimeSeriesProjection []ProjectedState   `json:"timeSeriesProjection,omitempty" yaml:"timeSeriesProjection,omitempty"`
	RiskWarnings         []string           `json:"riskWarnings,omitempty" yaml:"riskWarnings,omitempty"`
}

// SimulationState is a snapshot of key analysis fields for before/after comparison.
type SimulationState struct {
	DesiredReplicas int32    `json:"desiredReplicas" yaml:"desiredReplicas"`
	Health          string   `json:"health" yaml:"health"`
	HealthScore     int      `json:"healthScore" yaml:"healthScore"`
	Summary         string   `json:"summary" yaml:"summary"`
	ScalingLimited  bool     `json:"scalingLimited" yaml:"scalingLimited"`
	Metrics         []Metric `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// ProjectedState holds a single point in a time-series projection showing
// estimated replica count at a given time offset.
type ProjectedState struct {
	TimeOffset           int32   `json:"timeOffset" yaml:"timeOffset"`
	ProjectedReplicas    int32   `json:"projectedReplicas" yaml:"projectedReplicas"`
	ProjectedMetricRatio float64 `json:"projectedMetricRatio,omitempty" yaml:"projectedMetricRatio,omitempty"`
}

// SimulationExtendedOptions configures extended simulation with time-series
// projection and additional parameter overrides.
type SimulationExtendedOptions struct {
	DurationSeconds int32 `json:"durationSeconds" yaml:"durationSeconds"`
	StepSeconds     int32 `json:"stepSeconds" yaml:"stepSeconds"`
}

// MetricSimulation holds the result of simulating a metric value change.
type MetricSimulation struct {
	// MetricName is the name of the simulated metric.
	MetricName string `json:"metricName" yaml:"metricName"`
	// OriginalValue is the current metric value before simulation.
	OriginalValue string `json:"originalValue" yaml:"originalValue"`
	// SimulatedValue is the simulated metric value.
	SimulatedValue string `json:"simulatedValue" yaml:"simulatedValue"`
	// ProjectedRatio is the estimated ratio after simulation.
	ProjectedRatio *float64 `json:"projectedRatio,omitempty" yaml:"projectedRatio,omitempty"`
	// ProjectedReplicas is the estimated desired replica count.
	ProjectedReplicas int32 `json:"projectedReplicas" yaml:"projectedReplicas"`
	// ToleranceImpact describes whether tolerance would suppress this change.
	ToleranceImpact string `json:"toleranceImpact,omitempty" yaml:"toleranceImpact,omitempty"`
	// StabilizationImpact describes whether stabilization would delay this change.
	StabilizationImpact string `json:"stabilizationImpact,omitempty" yaml:"stabilizationImpact,omitempty"`
	// RiskAssessment for this specific metric simulation.
	RiskAssessment string `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
}
