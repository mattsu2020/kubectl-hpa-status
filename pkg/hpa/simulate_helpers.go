package hpa

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

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
		return 0, fmt.Errorf("%w: targetAverageUtilization must be > 0, got %d", ErrInvalidSimulationValue, v)
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
	normalizedPath := normalizeSimulationPath(path)
	if definition, ok := simulationOverrideDefinitions[normalizedPath]; ok {
		return definition.Original(hpa)
	}
	if strings.HasPrefix(normalizedPath, "metric.") && strings.HasSuffix(normalizedPath, ".target") {
		name := strings.TrimSuffix(strings.TrimPrefix(normalizedPath, "metric."), ".target")
		if spec, found := resolveMetricSpec(hpa, name); found {
			if target := metricTargetPointer(&spec); target != nil {
				return FormatMetricTarget(*target)
			}
		}
	}
	return "<unknown>"
}

func originalDirectionalTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, scaleUp bool) string {
	ratio := 0.0
	if scaleUp {
		ratio = 2
	}
	value, configured := directionalTolerance(hpa, ratio)
	if !configured {
		return fmt.Sprintf("%.3g (default)", value)
	}
	return fmt.Sprintf("%.3g", value)
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
