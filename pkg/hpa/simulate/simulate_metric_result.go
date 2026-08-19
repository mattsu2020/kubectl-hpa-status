package simulate

import (
	"errors"
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// buildMetricSimulation creates a MetricSimulation for a single override.
func buildMetricSimulation(original, modified *autoscalingv2.HorizontalPodAutoscaler, name, value string, _, after SimulationState) MetricSimulation {
	ms := MetricSimulation{
		MetricName:        name,
		SimulatedValue:    value,
		ProjectedReplicas: after.DesiredReplicas,
	}

	// Find original value
	spec, specErr := resolveMetricSpecUnique(original, name)
	if specErr != nil {
		if errors.Is(specErr, ErrMetricAmbiguous) {
			ms.OriginalValue = "<ambiguous metric name>"
			return ms
		}
		ms.OriginalValue = "<not found>"
		return ms
	}

	idx, currentErr := findCurrentMetricForSpec(original, spec)
	if currentErr != nil || idx < 0 {
		ms.OriginalValue = "<no current value>"
		return ms
	}

	ms.OriginalValue = formatMetricValue(original.Status.CurrentMetrics[idx], spec.Type)

	modifiedIdx, modifiedErr := findCurrentMetricForSpec(modified, spec)
	if modifiedErr != nil || modifiedIdx < 0 {
		return ms
	}
	_, ratio := metricImpactRatioInvoker(modified, modified.Status.CurrentMetrics[modifiedIdx])
	if ratio != nil {
		ms.ProjectedRatio = ratio
		projected, projectable := estimatedSimulatedMetricDesired(
			modified,
			modified.Status.CurrentMetrics[modifiedIdx],
			*ratio,
		)
		if !projectable {
			return ms
		}
		minReplicas := int32(1)
		if modified.Spec.MinReplicas != nil {
			minReplicas = *modified.Spec.MinReplicas
		}
		projected, _, _ = normalizeSimulatedDesired(
			modified,
			projected,
			minReplicas,
			modified.Spec.MaxReplicas,
		)
		ms.ProjectedReplicas = projected
		within, tolerance := ratioWithinToleranceInvoker(modified, *ratio)
		if within {
			ms.ToleranceImpact = fmt.Sprintf("%s tolerance %.3f suppresses scaling", toleranceDirectionInvoker(*ratio, nil, nil), tolerance)
		} else {
			ms.ToleranceImpact = fmt.Sprintf("outside %s tolerance %.3f", toleranceDirectionInvoker(*ratio, nil, nil), tolerance)
		}
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		ms.SimulatedValue = formatMetricValue(modified.Status.CurrentMetrics[modifiedIdx], spec.Type)
	}

	return ms
}

// formatMetricValue returns a display string for a current metric value.
func formatMetricValue(metric autoscalingv2.MetricStatus, metricType autoscalingv2.MetricSourceType) string {
	var value *autoscalingv2.MetricValueStatus
	switch metricType {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil {
			value = &metric.Resource.Current
		}
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External != nil {
			value = &metric.External.Current
		}
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods != nil {
			value = &metric.Pods.Current
		}
	case autoscalingv2.ObjectMetricSourceType:
		if metric.Object != nil {
			value = &metric.Object.Current
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if metric.ContainerResource != nil {
			value = &metric.ContainerResource.Current
		}
	}
	if value != nil {
		return formatMetricValueStatus(*value)
	}
	return "<unknown>"
}

// buildMetricSimulationInterpretation generates interpretation lines comparing
// before/after states with metric-specific observations.
func buildMetricSimulationInterpretation(before, after *SimulationState, simulations []MetricSimulation) []string {
	var lines []string

	if before.DesiredReplicas != after.DesiredReplicas {
		lines = append(lines, fmt.Sprintf("desiredReplicas would change from %d to %d", before.DesiredReplicas, after.DesiredReplicas))
	} else {
		lines = append(lines, fmt.Sprintf("desiredReplicas unchanged at %d", before.DesiredReplicas))
	}

	for _, ms := range simulations {
		if ms.ProjectedRatio != nil {
			ratio := *ms.ProjectedRatio
			switch {
			case ratio > 1.0:
				lines = append(lines, fmt.Sprintf("%s: value %.2fx above target, projected %d replicas", ms.MetricName, ratio, ms.ProjectedReplicas))
			case ratio < 1.0:
				lines = append(lines, fmt.Sprintf("%s: value %.2fx below target, projected %d replicas", ms.MetricName, ratio, ms.ProjectedReplicas))
			default:
				lines = append(lines, fmt.Sprintf("%s: at target, projected %d replicas", ms.MetricName, ms.ProjectedReplicas))
			}
		}
	}

	if before.Health != after.Health {
		lines = append(lines, fmt.Sprintf("health would change from %s (%d) to %s (%d)", before.Health, before.HealthScore, after.Health, after.HealthScore))
	}

	if before.ScalingLimited && !after.ScalingLimited {
		lines = append(lines, "ScalingLimited condition would be resolved")
	} else if !before.ScalingLimited && after.ScalingLimited {
		lines = append(lines, "Warning: ScalingLimited condition would appear")
	}

	return lines
}

// assessMetricSimulationRisk generates risk assessment text for metric simulations.
func assessMetricSimulationRisk(original, _ *autoscalingv2.HorizontalPodAutoscaler, simulations []MetricSimulation) string {
	var risks []string

	for _, ms := range simulations {
		if ms.ProjectedRatio != nil {
			ratio := *ms.ProjectedRatio
			if ratio >= 2.0 {
				risks = append(risks, fmt.Sprintf("%s at %.1fx target is very high; verify the workload can tolerate this pressure and that node capacity is available", ms.MetricName, ratio))
			}
			minReplicas := int32(1)
			if original.Spec.MinReplicas != nil {
				minReplicas = *original.Spec.MinReplicas
			}
			if ms.ProjectedReplicas >= original.Spec.MaxReplicas {
				risks = append(risks, fmt.Sprintf("%s would reach maxReplicas=%d; consider raising maxReplicas if demand is genuine", ms.MetricName, original.Spec.MaxReplicas))
			}
			if ratio <= 0.5 && minReplicas > 0 {
				risks = append(risks, fmt.Sprintf("%s at %.1fx target is very low; scale-down may be rapid if stabilization window is short", ms.MetricName, ratio))
			}
		}
	}

	if len(risks) == 0 {
		return ""
	}
	return strings.Join(risks, "; ")
}
