package hpa

// HealthState represents the computed health state of an HPA.
// Serialization values remain "OK", "ERROR", "LIMITED", "STABILIZED"
// for backward compatibility with existing JSON/YAML output.
type HealthState string

const (
	// HealthOK indicates no issues detected.
	HealthOK HealthState = "OK"
	// HealthError indicates ScalingActive not True or AbleToScale not True.
	HealthError HealthState = "ERROR"
	// HealthLimited indicates ScalingLimited is True or implicit max-replicas ceiling.
	HealthLimited HealthState = "LIMITED"
	// HealthStabilized indicates ScaleDownStabilized with no ERROR or LIMITED.
	HealthStabilized HealthState = "STABILIZED"
)

// HealthState returns the typed health state for a.Health without changing the
// JSON wire type of Analysis.Health (still a plain string for compatibility).
// Empty or unknown values are returned as HealthState(a.Health) unchanged so
// callers can compare against the Health* constants.
func (a Analysis) HealthState() HealthState {
	return HealthState(a.Health)
}

// HealthSignal records a single penalty signal that contributed to the final
// health state. Signals are exposed via --debug and -o json for transparency.
type HealthSignal struct {
	Reason   string      `json:"reason" yaml:"reason"`
	Penalty  int         `json:"penalty" yaml:"penalty"`
	Severity HealthState `json:"severity" yaml:"severity"`
}

// HealthResult holds the final health state, score, and the individual signals
// that contributed to the score.
type HealthResult struct {
	State   HealthState    `json:"health" yaml:"health"`
	Score   int            `json:"healthScore" yaml:"healthScore"`
	Signals []HealthSignal `json:"signals,omitempty" yaml:"signals,omitempty"`
}
