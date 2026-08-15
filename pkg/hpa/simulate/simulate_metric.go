package simulate

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// MetricChange simulates metric value changes and returns a
// SimulationResult with projected replica counts and impact analysis.
// The original HPA is not mutated; a deep copy is used internally.
func MetricChange(hpa *autoscalingv2.HorizontalPodAutoscaler, metricOverrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	return Scenario(hpa, nil, metricOverrides, weights, SimulationExtendedOptions{})
}

// applyMetricOverride modifies the current metric value on the deep-copied HPA.
// Supported formats:
//   - cpu=80% or cpu=80 — sets utilization for resource metric
//   - memory=4Gi — sets averageValue for resource metric
//   - http_requests=500 — sets value for external/pods metric
//   - cpu=+20% — relative increase from current value
//   - cpu=-10% — relative decrease from current value
func applyMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, name, value string) error {
	spec, err := resolveMetricSpecUnique(hpa, name)
	if err != nil {
		return fmt.Errorf("metric %q: %w", name, err)
	}

	idx, err := findCurrentMetricForSpec(hpa, spec)
	if err != nil {
		return fmt.Errorf("metric %q current value: %w", name, err)
	}
	if idx < 0 {
		return fmt.Errorf("metric %q has no current value in HPA status; cannot simulate without a baseline", name)
	}

	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return applyRelativeOverride(hpa, spec, idx, value)
	}

	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return ApplyResourceMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.ContainerResourceMetricSourceType:
		return ApplyContainerResourceMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.ExternalMetricSourceType:
		return ApplyExternalMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.PodsMetricSourceType:
		return ApplyPodsMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.ObjectMetricSourceType:
		return ApplyObjectMetricOverride(hpa, spec, idx, value)
	default:
		return fmt.Errorf("unsupported metric type %q for metric %q", spec.Type, name)
	}
}
