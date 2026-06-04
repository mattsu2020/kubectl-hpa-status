package hpa

import (
	"fmt"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// SimulateHPA creates a deep copy of the HPA, applies the given overrides, and
// compares the analysis of the modified HPA against the original. Returns a
// SimulationResult describing the before/after state, or an error if the
// overrides are invalid.
func SimulateHPA(hpa *autoscalingv2.HorizontalPodAutoscaler, overrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	if hpa == nil {
		return nil, fmt.Errorf("HPA must not be nil")
	}

	beforeAnalysis := AnalyzeWithOptions(hpa, true, AnalysisOptions{HealthWeights: weights})
	before := simulationStateFromAnalysis(&beforeAnalysis)

	modified := hpa.DeepCopy()
	for path, value := range overrides {
		if err := applySimulationOverride(modified, path, value); err != nil {
			return nil, fmt.Errorf("override %s=%s: %w", path, value, err)
		}
	}

	afterAnalysis := AnalyzeWithOptions(modified, true, AnalysisOptions{HealthWeights: weights})
	after := simulationStateFromAnalysis(&afterAnalysis)

	result := &SimulationResult{
		Before: before,
		After:  after,
	}

	if len(overrides) == 1 {
		for path, value := range overrides {
			result.Parameter = path
			result.SimulatedValue = value
			result.OriginalValue = originalValue(hpa, path)
		}
	} else {
		parts := make([]string, 0, len(overrides))
		for k, v := range overrides {
			parts = append(parts, k+"="+v)
		}
		result.Parameter = strings.Join(parts, ", ")
	}

	result.Interpretation = buildSimulationInterpretation(&before, &after, modified)
	result.RiskAssessment = assessSimulationRisk(hpa, modified, &before, &after)

	return result, nil
}

// simulationStateFromAnalysis extracts the key fields for simulation comparison.
func simulationStateFromAnalysis(a *Analysis) SimulationState {
	limited := false
	for _, c := range a.Conditions {
		if c.Type == "ScalingLimited" && c.Status == "True" {
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
	}
}

// applySimulationOverride modifies a single field on the HPA spec using dot-notation path.
func applySimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, path, value string) error {
	switch strings.ToLower(path) {
	case "maxreplicas":
		v, err := parseInt32(value)
		if err != nil {
			return err
		}
		if v < 1 {
			return fmt.Errorf("maxReplicas must be >= 1")
		}
		hpa.Spec.MaxReplicas = v
	case "minreplicas":
		v, err := parseInt32(value)
		if err != nil {
			return err
		}
		hpa.Spec.MinReplicas = &v
	case "scaledown.stabilizationwindowseconds":
		v, err := parseInt32(value)
		if err != nil {
			return err
		}
		if v < 0 {
			return fmt.Errorf("stabilizationWindowSeconds must be >= 0")
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds = &v
	case "scaleup.stabilizationwindowseconds":
		v, err := parseInt32(value)
		if err != nil {
			return err
		}
		if v < 0 {
			return fmt.Errorf("stabilizationWindowSeconds must be >= 0")
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds = &v
	case "scaledown.selectpolicy":
		ensureBehavior(hpa)
		p := selectPolicy(value)
		hpa.Spec.Behavior.ScaleDown.SelectPolicy = &p
	case "scaleup.selectpolicy":
		ensureBehavior(hpa)
		p := selectPolicy(value)
		hpa.Spec.Behavior.ScaleUp.SelectPolicy = &p
	default:
		return fmt.Errorf("unsupported path %q; supported: maxReplicas, minReplicas, scaleDown.stabilizationWindowSeconds, scaleUp.stabilizationWindowSeconds, scaleDown.selectPolicy, scaleUp.selectPolicy", path)
	}
	return nil
}

// ensureBehavior initializes the behavior struct if nil.
func ensureBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	if hpa.Spec.Behavior == nil {
		hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
	}
	if hpa.Spec.Behavior.ScaleDown == nil {
		hpa.Spec.Behavior.ScaleDown = &autoscalingv2.HPAScalingRules{}
	}
	if hpa.Spec.Behavior.ScaleUp == nil {
		hpa.Spec.Behavior.ScaleUp = &autoscalingv2.HPAScalingRules{}
	}
}

// selectPolicy converts a string value to a valid scaling policy.
func selectPolicy(value string) autoscalingv2.ScalingPolicySelect {
	switch strings.ToLower(value) {
	case "max":
		return autoscalingv2.ScalingPolicySelect("Max")
	case "min":
		return autoscalingv2.ScalingPolicySelect("Min")
	case "disabled":
		return autoscalingv2.ScalingPolicySelect("Disabled")
	default:
		return autoscalingv2.ScalingPolicySelect(value)
	}
}

// parseInt32 parses a string as int32.
func parseInt32(value string) (int32, error) {
	v, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", value, err)
	}
	return int32(v), nil
}

// originalValue returns the current value for the given path on the original HPA.
func originalValue(hpa *autoscalingv2.HorizontalPodAutoscaler, path string) string {
	switch strings.ToLower(path) {
	case "maxreplicas":
		return fmt.Sprintf("%d", hpa.Spec.MaxReplicas)
	case "minreplicas":
		if hpa.Spec.MinReplicas != nil {
			return fmt.Sprintf("%d", *hpa.Spec.MinReplicas)
		}
		return "1"
	case "scaledown.stabilizationwindowseconds":
		if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
			return fmt.Sprintf("%d", *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
		}
		return "300"
	case "scaleup.stabilizationwindowseconds":
		if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil {
			return fmt.Sprintf("%d", *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds)
		}
		return "0"
	default:
		return "<unknown>"
	}
}

// buildSimulationInterpretation generates interpretation lines comparing before/after states.
func buildSimulationInterpretation(before, after *SimulationState, modified *autoscalingv2.HorizontalPodAutoscaler) []string {
	var lines []string

	if before.DesiredReplicas != after.DesiredReplicas {
		lines = append(lines, fmt.Sprintf("desiredReplicas would change from %d to %d", before.DesiredReplicas, after.DesiredReplicas))
	} else {
		lines = append(lines, fmt.Sprintf("desiredReplicas unchanged at %d", before.DesiredReplicas))
	}

	if before.Health != after.Health {
		lines = append(lines, fmt.Sprintf("health would change from %s (%d) to %s (%d)", before.Health, before.HealthScore, after.Health, after.HealthScore))
	}

	if before.ScalingLimited && !after.ScalingLimited {
		lines = append(lines, "ScalingLimited condition would be resolved")
	} else if !before.ScalingLimited && after.ScalingLimited {
		lines = append(lines, "Warning: SimulationLimited condition would appear")
	}

	if modified.Spec.MaxReplicas > 0 && before.DesiredReplicas >= modified.Spec.MaxReplicas {
		lines = append(lines, fmt.Sprintf("desiredReplicas=%d still at or above new maxReplicas=%d; further increase may be needed", after.DesiredReplicas, modified.Spec.MaxReplicas))
	}

	return lines
}

// assessSimulationRisk generates risk assessment text for the simulation.
func assessSimulationRisk(original, modified *autoscalingv2.HorizontalPodAutoscaler, before, after *SimulationState) string {
	var risks []string

	if modified.Spec.MaxReplicas > original.Spec.MaxReplicas {
		ratio := float64(modified.Spec.MaxReplicas) / float64(original.Spec.MaxReplicas)
		risks = append(risks, fmt.Sprintf("Raising maxReplicas from %d to %d (%.1fx capacity); verify node and quota headroom", original.Spec.MaxReplicas, modified.Spec.MaxReplicas, ratio))
	}

	if modified.Spec.MinReplicas != nil && original.Spec.MinReplicas != nil {
		if *modified.Spec.MinReplicas < *original.Spec.MinReplicas {
			risks = append(risks, fmt.Sprintf("Lowering minReplicas from %d to %d may reduce availability during low-traffic periods", *original.Spec.MinReplicas, *modified.Spec.MinReplicas))
		}
		if *modified.Spec.MinReplicas > *original.Spec.MinReplicas {
			risks = append(risks, fmt.Sprintf("Raising minReplicas from %d to %d increases baseline resource consumption", *original.Spec.MinReplicas, *modified.Spec.MinReplicas))
		}
	}

	if modified.Spec.Behavior != nil && modified.Spec.Behavior.ScaleDown != nil &&
		modified.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		window := *modified.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
		if window < 60 {
			risks = append(risks, fmt.Sprintf("Reducing scaleDown stabilization to %ds may cause thrashing; monitor for flapping", window))
		}
	}

	if len(risks) == 0 {
		return ""
	}
	return strings.Join(risks, "; ")
}
