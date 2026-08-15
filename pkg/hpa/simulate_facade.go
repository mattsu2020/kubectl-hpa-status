package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/simulate"
)

// This file is a thin re-export facade for the simulation domain, which now
// lives in pkg/hpa/simulate. The types and functions below preserve the
// existing hpaanalysis.* API surface so cmd/ and internal/ callers keep
// compiling without changing their imports. The canonical implementations are
// in pkg/hpa/simulate/simulate.go and its siblings.

// Simulation domain type aliases. The canonical types live in
// pkg/hpa/simulate without the Simulation prefix where the historical name
// already carried it.
type (
	// SimulationResult aliases simulate.SimulationResult.
	//
	// Deprecated: Use simulate.SimulationResult instead. Scheduled for removal in v3.0.0.
	SimulationResult = simulate.SimulationResult
	// SimulationState aliases simulate.SimulationState.
	//
	// Deprecated: Use simulate.SimulationState instead. Scheduled for removal in v3.0.0.
	SimulationState = simulate.SimulationState
	// SimulationExtendedOptions aliases simulate.SimulationExtendedOptions.
	//
	// Deprecated: Use simulate.SimulationExtendedOptions instead. Scheduled for removal in v3.0.0.
	SimulationExtendedOptions = simulate.SimulationExtendedOptions
	// ProjectedState aliases simulate.ProjectedState.
	//
	// Deprecated: Use simulate.ProjectedState instead. Scheduled for removal in v3.0.0.
	ProjectedState = simulate.ProjectedState
)

// Simulation error sentinels. They alias the simulate package sentinels so
// errors.Is keeps working across the facade.
var (
	// ErrInvalidSimulationValue aliases simulate.ErrInvalidSimulationValue.
	//
	// Deprecated: Use simulate.ErrInvalidSimulationValue instead. Scheduled for removal in v3.0.0.
	ErrInvalidSimulationValue = simulate.ErrInvalidSimulationValue
	// ErrUnsupportedSimulationSemantics aliases simulate.ErrUnsupportedSimulationSemantics.
	//
	// Deprecated: Use simulate.ErrUnsupportedSimulationSemantics instead. Scheduled for removal in v3.0.0.
	ErrUnsupportedSimulationSemantics = simulate.ErrUnsupportedSimulationSemantics
)

// SimulateHPA mirrors simulate.HPA, converting the caller's
// HealthWeights to the simulate package representation.
//
// Deprecated: Use simulate.HPA instead. Scheduled for removal in v3.0.0.
func SimulateHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	return simulate.HPA(hpa, overrides, healthWeightsForSimulate(weights))
}

// SimulateScenario mirrors simulate.Scenario, converting the caller's
// HealthWeights to the simulate package representation.
//
// Deprecated: Use simulate.Scenario instead. Scheduled for removal in v3.0.0.
func SimulateScenario(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides, metricOverrides map[string]string, weights HealthWeights, extOpts SimulationExtendedOptions) (*SimulationResult, error) {
	return simulate.Scenario(hpa, overrides, metricOverrides, healthWeightsForSimulate(weights), extOpts)
}

// SimulateExtended mirrors simulate.Extended, converting the caller's
// HealthWeights to the simulate package representation.
//
// Deprecated: Use simulate.Extended instead. Scheduled for removal in v3.0.0.
func SimulateExtended(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides map[string]string, weights HealthWeights, extOpts SimulationExtendedOptions) (*SimulationResult, error) {
	return simulate.Extended(hpa, overrides, healthWeightsForSimulate(weights), extOpts)
}

// SimulateMetricChange mirrors simulate.MetricChange, converting the
// caller's HealthWeights to the simulate package representation.
//
// Deprecated: Use simulate.MetricChange instead. Scheduled for removal in v3.0.0.
func SimulateMetricChange(hpa *autoscalingv2.HorizontalPodAutoscaler, metricOverrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	return simulate.MetricChange(hpa, metricOverrides, healthWeightsForSimulate(weights))
}

// BuildSimulatedHPA aliases simulate.BuildSimulatedHPA.
//
// Deprecated: Use simulate.BuildSimulatedHPA instead. Scheduled for removal in v3.0.0.
func BuildSimulatedHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides, metricOverrides map[string]string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return simulate.BuildSimulatedHPA(hpa, overrides, metricOverrides)
}

// FormatTrajectoryASCII aliases simulate.FormatTrajectoryASCII.
//
// Deprecated: Use simulate.FormatTrajectoryASCII instead. Scheduled for removal in v3.0.0.
func FormatTrajectoryASCII(states []ProjectedState, width int) string {
	return simulate.FormatTrajectoryASCII(states, width)
}

// healthWeightsForSimulate converts HealthWeights to the simulate package's
// representation. The simulate package uses plain ints where 0 selects the
// default penalty, so a nil weight (default) and an explicit 0 (disable) both
// map to the default here; the explicit-disable distinction currently only
// survives on the AnalysisOptions path used by AnalyzeWithOptions.
func healthWeightsForSimulate(w HealthWeights) simulate.HealthWeights {
	deref := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	return simulate.HealthWeights{
		Limited:  deref(w.ScalingLimited),
		NotReady: deref(w.UnableToScale),
		Falling:  deref(w.ScaleDownStabilized),
	}
}
