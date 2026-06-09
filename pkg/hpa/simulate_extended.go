package hpa

import (
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// SimulateExtended wraps the existing simulation with time-series projection
// and extended risk assessment. It does not mutate the original HPA.
func SimulateExtended(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides map[string]string, weights HealthWeights, extOpts SimulationExtendedOptions) (*SimulationResult, error) {
	if hpa == nil {
		return nil, fmt.Errorf("HPA must not be nil")
	}

	// Run the base simulation.
	result, err := SimulateHPA(hpa, overrides, weights)
	if err != nil {
		return nil, err
	}

	// Build time-series projection if duration is specified.
	if extOpts.DurationSeconds > 0 {
		modified := hpa.DeepCopy()
		for path, value := range overrides {
			if err := applySimulationOverride(modified, path, value); err != nil {
				return nil, fmt.Errorf("override %s=%s: %w", path, value, err)
			}
		}
		result.TimeSeriesProjection = ProjectReplicaTrajectory(hpa, modified, extOpts)
	}

	// Extended risk assessment.
	result.RiskWarnings = assessExtendedRisk(hpa, overrides, result)

	return result, nil
}

// assessExtendedRisk generates risk warnings based on the simulation parameters
// and results. It augments the base risk assessment with additional checks.
func assessExtendedRisk(original *autoscalingv2.HorizontalPodAutoscaler, overrides map[string]string, result *SimulationResult) []string {
	var warnings []string

	// Check for aggressive scale-down stabilization window.
	if modified := getOverrideValue(overrides, "scaledown.stabilizationwindowseconds"); modified != "" {
		var window int
		if _, err := fmt.Sscanf(modified, "%d", &window); err == nil && window < 60 {
			warnings = append(warnings, fmt.Sprintf("Reducing scaleDown stabilization to %ds may cause thrashing; monitor for flapping", window))
		}
	}

	// Check for large replica swings.
	if result.Before.DesiredReplicas > 0 {
		delta := math.Abs(float64(result.After.DesiredReplicas - result.Before.DesiredReplicas))
		ratio := delta / float64(result.Before.DesiredReplicas)
		if ratio > 0.5 {
			warnings = append(warnings, fmt.Sprintf("Large replica swing: %d → %d (%.0f%% change); verify cluster capacity",
				result.Before.DesiredReplicas, result.After.DesiredReplicas, ratio*100))
		}
	}

	// Check for hitting min/max boundaries.
	minReplicas := int32(1)
	if original.Spec.MinReplicas != nil {
		minReplicas = *original.Spec.MinReplicas
	}
	if result.After.DesiredReplicas >= original.Spec.MaxReplicas {
		warnings = append(warnings, fmt.Sprintf("Projected replicas=%d at maxReplicas=%d; further scale-out would be blocked",
			result.After.DesiredReplicas, original.Spec.MaxReplicas))
	}
	if result.After.DesiredReplicas <= minReplicas {
		warnings = append(warnings, fmt.Sprintf("Projected replicas=%d at minReplicas=%d; further scale-in would be blocked",
			result.After.DesiredReplicas, minReplicas))
	}

	// Check health degradation.
	if result.After.HealthScore < result.Before.HealthScore {
		delta := result.Before.HealthScore - result.After.HealthScore
		if delta >= 20 {
			warnings = append(warnings, fmt.Sprintf("Health score would drop by %d points (%s → %s); consider adjusting parameters",
				delta, result.Before.Health, result.After.Health))
		}
	}

	return warnings
}

// getOverrideValue extracts a case-insensitive override value from the map.
func getOverrideValue(overrides map[string]string, key string) string {
	for k, v := range overrides {
		if caseInsensitiveEqual(k, key) {
			return v
		}
	}
	return ""
}

// caseInsensitiveEqual compares two strings case-insensitively,
// normalizing dots and underscores.
func caseInsensitiveEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca := a[i]
		cb := b[i]
		if ca == '_' {
			ca = '.'
		}
		if cb == '_' {
			cb = '.'
		}
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
