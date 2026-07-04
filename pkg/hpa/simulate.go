package hpa

import (
	"errors"
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
		return nil, ErrNilHPA
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
	result.Confidence = "estimated"

	return result, nil
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

// applySimulationOverride modifies a single field on the HPA spec using dot-notation path.
func applySimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, path, value string) error {
	switch normalizeSimulationPath(path) {
	case "maxreplicas":
		v, err := parseNonNegativeInt32(value, 1, "maxReplicas must be >= 1")
		if err != nil {
			return err
		}
		hpa.Spec.MaxReplicas = v
	case "minreplicas":
		v, err := parseInt32(value)
		if err != nil {
			return err
		}
		hpa.Spec.MinReplicas = &v
	case "scaledown.stabilizationwindowseconds":
		v, err := parseNonNegativeInt32(value, 0, "stabilizationWindowSeconds must be >= 0")
		if err != nil {
			return err
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds = &v
	case "scaleup.stabilizationwindowseconds":
		v, err := parseNonNegativeInt32(value, 0, "stabilizationWindowSeconds must be >= 0")
		if err != nil {
			return err
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
	case "targetaverageutilization":
		v, err := parsePositiveInt32(value)
		if err != nil {
			return err
		}
		applyAverageUtilizationToResourceMetrics(hpa, v)
	case "tolerance":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("invalid tolerance %q: %w", value, err)
		}
		// Kubernetes HPA tolerance is controller-wide in current APIs. Keep this
		// accepted so extended projections can describe the intended scenario.
	default:
		return fmt.Errorf("unsupported path %q; supported: maxReplicas, minReplicas, scaleDown.stabilizationWindowSeconds, scaleDown.stabilizationWindow, scaleUp.stabilizationWindowSeconds, scaleUp.stabilizationWindow, scaleDown.selectPolicy, scaleUp.selectPolicy, targetAverageUtilization, tolerance", path)
	}
	return nil
}

// parseNonNegativeInt32 parses value as int32 and requires v >= minVal, returning a descriptive error otherwise.
func parseNonNegativeInt32(value string, minVal int32, errMsg string) (int32, error) {
	v, err := parseInt32(value)
	if err != nil {
		return 0, err
	}
	if v < minVal {
		return 0, errors.New(errMsg)
	}
	return v, nil
}

// parsePositiveInt32 parses value as int32 and requires v > 0.
func parsePositiveInt32(value string) (int32, error) {
	v, err := parseInt32(value)
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("targetAverageUtilization must be > 0")
	}
	return v, nil
}

// applyAverageUtilizationToResourceMetrics sets the target average utilization on every resource metric source.
func applyAverageUtilizationToResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler, v int32) {
	for i := range hpa.Spec.Metrics {
		if hpa.Spec.Metrics[i].Type == autoscalingv2.ResourceMetricSourceType {
			hpa.Spec.Metrics[i].Resource.Target.AverageUtilization = &v
			hpa.Spec.Metrics[i].Resource.Target.Type = autoscalingv2.UtilizationMetricType
		}
	}
}

func normalizeSimulationPath(path string) string {
	p := strings.ToLower(strings.TrimSpace(path))
	switch p {
	case "scaledown.stabilizationwindow":
		return "scaledown.stabilizationwindowseconds"
	case "scaleup.stabilizationwindow":
		return "scaleup.stabilizationwindowseconds"
	default:
		return p
	}
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
	switch normalizeSimulationPath(path) {
	case "maxreplicas":
		return fmt.Sprintf("%d", hpa.Spec.MaxReplicas)
	case "minreplicas":
		return originalMinReplicas(hpa)
	case "scaledown.stabilizationwindowseconds":
		return originalScaleDownStabilizationWindow(hpa)
	case "scaleup.stabilizationwindowseconds":
		return originalScaleUpStabilizationWindow(hpa)
	case "targetaverageutilization":
		return originalTargetAverageUtilization(hpa)
	case "tolerance":
		return "<controller default>"
	default:
		return "<unknown>"
	}
}

func originalMinReplicas(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.MinReplicas != nil {
		return fmt.Sprintf("%d", *hpa.Spec.MinReplicas)
	}
	return "1"
}

func originalScaleDownStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		return fmt.Sprintf("%d", *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
	}
	return "300"
}

func originalScaleUpStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil {
		return fmt.Sprintf("%d", *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds)
	}
	return "0"
}

func originalTargetAverageUtilization(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource.Target.AverageUtilization != nil {
			return fmt.Sprintf("%d", *m.Resource.Target.AverageUtilization)
		}
	}
	return "<not set>"
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
func assessSimulationRisk(original, modified *autoscalingv2.HorizontalPodAutoscaler, _ *SimulationState, _ *SimulationState) string {
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
