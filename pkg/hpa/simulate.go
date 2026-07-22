package hpa

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// Sentinel errors for simulation override validation so callers can branch
// with errors.Is instead of matching message text.
var (
	// ErrUnsupportedSimulationPath is returned when an override path is not
	// one of the supported dot-notation fields.
	ErrUnsupportedSimulationPath = errors.New("unsupported path")
	// ErrInvalidSimulationValue is returned when an override value fails
	// validation for its target field (range, sign, format).
	ErrInvalidSimulationValue = errors.New("invalid simulation value")
)

// SimulateHPA creates a deep copy of the HPA, applies the given overrides, and
// compares the analysis of the modified HPA against the original. Returns a
// SimulationResult describing the before/after state, or an error if the
// overrides are invalid.
func SimulateHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	return SimulateScenario(hpa, overrides, nil, weights, SimulationExtendedOptions{})
}

// SimulateScenario evaluates spec and current-metric overrides together. The
// projected desired replica count is recomputed from all visible metrics using
// the maximum per-metric recommendation, directional tolerance, and min/max
// bounds. The original HPA is never mutated.
func SimulateScenario(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides, metricOverrides map[string]string, weights HealthWeights, extOpts SimulationExtendedOptions) (*SimulationResult, error) {
	if hpa == nil {
		return nil, ErrNilHPA
	}

	beforeAnalysis := AnalyzeWithOptions(hpa, true, AnalysisOptions{HealthWeights: weights})
	before := simulationStateFromAnalysis(&beforeAnalysis)

	modified, err := BuildSimulatedHPA(hpa, overrides, metricOverrides)
	if err != nil {
		return nil, err
	}

	afterAnalysis := AnalyzeWithOptions(modified, true, AnalysisOptions{HealthWeights: weights})
	after := simulationStateFromAnalysis(&afterAnalysis)

	result := &SimulationResult{
		Before: before,
		After:  after,
	}

	parameterCount := len(overrides) + len(metricOverrides)
	switch {
	case parameterCount == 1 && len(overrides) == 1:
		for _, path := range sortedMapKeys(overrides) {
			value := overrides[path]
			result.Parameter = path
			result.SimulatedValue = value
			result.OriginalValue = originalValue(hpa, path)
		}
	case parameterCount == 1:
		for _, name := range sortedMapKeys(metricOverrides) {
			value := metricOverrides[name]
			result.Parameter = "metric." + name
			result.SimulatedValue = value
			if idx, found := findCurrentMetric(hpa, name); found {
				spec, _ := resolveMetricSpec(hpa, name)
				result.OriginalValue = formatMetricValue(hpa.Status.CurrentMetrics[idx], spec.Type)
			}
		}
	default:
		parts := simulationParameterPairs(overrides, metricOverrides)
		result.Parameter = strings.Join(parts, ", ")
	}

	for _, name := range sortedMapKeys(metricOverrides) {
		result.MetricSimulations = append(result.MetricSimulations,
			buildMetricSimulation(hpa, modified, name, metricOverrides[name], before, after))
	}
	if len(result.MetricSimulations) > 0 {
		result.Interpretation = buildMetricSimulationInterpretation(&before, &after, result.MetricSimulations)
	} else {
		result.Interpretation = buildSimulationInterpretation(&before, &after, modified)
	}
	specRisk := assessSimulationRisk(hpa, modified, &before, &after)
	metricRisk := assessMetricSimulationRisk(hpa, modified, result.MetricSimulations)
	result.RiskAssessment = strings.Join(nonEmptyStrings(specRisk, metricRisk), "; ")
	result.Confidence = "estimated"
	if extOpts.DurationSeconds > 0 {
		result.TimeSeriesProjection = ProjectReplicaTrajectory(hpa, modified, extOpts)
	}
	result.RiskWarnings = assessExtendedRisk(hpa, overrides, result)

	return result, nil
}

// BuildSimulatedHPA returns a deep-copied HPA with all overrides applied and
// status.desiredReplicas replaced by the public-algorithm estimate. Callers use
// this for follow-on operations such as suggestions without mutating live data.
func BuildSimulatedHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides, metricOverrides map[string]string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	if hpa == nil {
		return nil, ErrNilHPA
	}
	modified := hpa.DeepCopy()
	for _, path := range sortedMapKeys(overrides) {
		value := overrides[path]
		if err := applySimulationOverride(modified, path, value); err != nil {
			return nil, fmt.Errorf("override %s=%s: %w", path, value, err)
		}
	}
	for _, name := range sortedMapKeys(metricOverrides) {
		value := metricOverrides[name]
		if err := applyMetricOverride(modified, name, value); err != nil {
			return nil, fmt.Errorf("metric override %s=%s: %w", name, value, err)
		}
	}
	if len(overrides) > 0 || len(metricOverrides) > 0 {
		recomputeSimulatedDesired(modified)
	}
	return modified, nil
}

func recomputeSimulatedDesired(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	desired := hpa.Status.DesiredReplicas
	found := false
	for _, metric := range hpa.Status.CurrentMetrics {
		_, ratio := metricImpactRatio(hpa, metric)
		if ratio == nil || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) || *ratio < 0 {
			continue
		}
		metricDesired := estimatedDesiredForRatio(hpa, *ratio)
		if !found || metricDesired > desired {
			desired = metricDesired
			found = true
		}
	}
	// Missing metrics conservatively block a scale-down in the public estimate.
	if found && len(hpa.Status.CurrentMetrics) < len(hpa.Spec.Metrics) && desired < hpa.Status.CurrentReplicas {
		desired = hpa.Status.CurrentReplicas
	}
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	if desired < minReplicas {
		desired = minReplicas
	}
	if desired > hpa.Spec.MaxReplicas {
		desired = hpa.Spec.MaxReplicas
	}
	hpa.Status.DesiredReplicas = desired
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func simulationParameterPairs(overrides, metricOverrides map[string]string) []string {
	parts := make([]string, 0, len(overrides)+len(metricOverrides))
	for _, key := range sortedMapKeys(overrides) {
		parts = append(parts, key+"="+overrides[key])
	}
	for _, key := range sortedMapKeys(metricOverrides) {
		parts = append(parts, "metric."+key+"="+metricOverrides[key])
	}
	return parts
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

// simulationStateFromAnalysis extracts the key fields for simulation comparison.
func simulationStateFromAnalysis(a *Analysis) SimulationState {
	limited := false
	for _, c := range a.Conditions {
		if c.Type == ConditionScalingLimited && c.Status == "True" {
			limited = true
			break
		}
	}
	return SimulationState{
		DesiredReplicas: a.Desired,
		Health:          a.Health,
		HealthScore:     a.HealthScore,
		Summary:         a.Summary,
		ScalingLimited:  limited,
		Metrics:         a.Metrics,
	}
}
