package hpa

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SimulateMetricChange simulates metric value changes and returns a
// SimulationResult with projected replica counts and impact analysis.
// The original HPA is not mutated; a deep copy is used internally.
func SimulateMetricChange(hpa *autoscalingv2.HorizontalPodAutoscaler, metricOverrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	if hpa == nil {
		return nil, fmt.Errorf("HPA must not be nil")
	}

	beforeAnalysis := AnalyzeWithOptions(hpa, true, AnalysisOptions{HealthWeights: weights})
	before := simulationStateFromAnalysis(&beforeAnalysis)

	modified := hpa.DeepCopy()
	for name, value := range metricOverrides {
		if err := applyMetricOverride(modified, name, value); err != nil {
			return nil, fmt.Errorf("metric override %s=%s: %w", name, value, err)
		}
	}

	afterAnalysis := AnalyzeWithOptions(modified, true, AnalysisOptions{HealthWeights: weights})
	after := simulationStateFromAnalysis(&afterAnalysis)

	simulations := make([]MetricSimulation, 0, len(metricOverrides))
	for name, value := range metricOverrides {
		ms := buildMetricSimulation(hpa, modified, name, value, before, after)
		simulations = append(simulations, ms)
	}

	result := &SimulationResult{
		Before:            before,
		After:             after,
		MetricSimulations: simulations,
	}

	result.Interpretation = buildMetricSimulationInterpretation(&before, &after, simulations)
	result.RiskAssessment = assessMetricSimulationRisk(hpa, modified, simulations)

	return result, nil
}

// applyMetricOverride modifies the current metric value on the deep-copied HPA.
// Supported formats:
//   - cpu=80% or cpu=80 — sets utilization for resource metric
//   - memory=4Gi — sets averageValue for resource metric
//   - http_requests=500 — sets value for external/pods metric
//   - cpu=+20% — relative increase from current value
//   - cpu=-10% — relative decrease from current value
func applyMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, name, value string) error {
	spec, found := resolveMetricSpec(hpa, name)
	if !found {
		return fmt.Errorf("metric %q not found in HPA spec", name)
	}

	idx, found := findCurrentMetric(hpa, name)
	if !found {
		return fmt.Errorf("metric %q has no current value in HPA status; cannot simulate without a baseline", name)
	}

	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return applyRelativeOverride(hpa, spec, idx, value)
	}

	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		resName := spec.Resource.Name
		switch {
		case strings.HasSuffix(value, "%"):
			parsed, err := strconv.ParseInt(strings.TrimSuffix(value, "%"), 10, 32)
			if err != nil {
				return fmt.Errorf("invalid utilization value %q: %w", value, err)
			}
			util := int32(parsed)
			hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
				Name: resName,
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &util,
				},
			}
		case isResourceQuantity(value):
			q := resource.MustParse(value)
			hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
				Name: resName,
				Current: autoscalingv2.MetricValueStatus{
					AverageValue: &q,
				},
			}
		default:
			parsed, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid resource metric value %q: expected utilization%%, quantity, or integer", value)
			}
			util := int32(parsed)
			hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
				Name: resName,
				Current: autoscalingv2.MetricValueStatus{
					AverageUtilization: &util,
				},
			}
		}
	case autoscalingv2.ExternalMetricSourceType:
		q := resource.MustParse(value)
		if spec.External.Metric.Selector.MatchLabels == nil &&
			hpa.Status.CurrentMetrics[idx].External != nil &&
			hpa.Status.CurrentMetrics[idx].External.Current.AverageValue != nil {
			hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     spec.External.Metric.Name,
					Selector: spec.External.Metric.Selector,
				},
				Current: autoscalingv2.MetricValueStatus{
					AverageValue: &q,
				},
			}
		} else {
			hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
				Metric: autoscalingv2.MetricIdentifier{
					Name:     spec.External.Metric.Name,
					Selector: spec.External.Metric.Selector,
				},
				Current: autoscalingv2.MetricValueStatus{
					Value: &q,
				},
			}
		}
	case autoscalingv2.PodsMetricSourceType:
		q := resource.MustParse(value)
		hpa.Status.CurrentMetrics[idx].Pods = &autoscalingv2.PodsMetricStatus{
			Metric: autoscalingv2.MetricIdentifier{
				Name:     spec.Pods.Metric.Name,
				Selector: spec.Pods.Metric.Selector,
			},
			Current: autoscalingv2.MetricValueStatus{
				AverageValue: &q,
			},
		}
	default:
		return fmt.Errorf("unsupported metric type %q for metric %q", spec.Type, name)
	}
	return nil
}

// applyRelativeOverride handles +/- percentage relative changes.
func applyRelativeOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		current := hpa.Status.CurrentMetrics[idx].Resource
		if current == nil || current.Current.AverageUtilization == nil {
			return fmt.Errorf("cannot apply relative change: no current utilization value for metric %q", spec.Resource.Name)
		}
		newVal, err := parseRelativeValue(value, *current.Current.AverageUtilization)
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name: spec.Resource.Name,
			Current: autoscalingv2.MetricValueStatus{
				AverageUtilization: &newVal,
			},
		}
	case autoscalingv2.ExternalMetricSourceType:
		current := hpa.Status.CurrentMetrics[idx].External
		if current == nil {
			return fmt.Errorf("cannot apply relative change: no current value for external metric %q", spec.External.Metric.Name)
		}
		currentQty := current.Current.Value
		if currentQty == nil {
			currentQty = current.Current.AverageValue
		}
		if currentQty == nil {
			return fmt.Errorf("cannot apply relative change: no current value for external metric %q", spec.External.Metric.Name)
		}
		newVal, err := parseRelativeQuantity(value, currentQty)
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
			Metric: autoscalingv2.MetricIdentifier{
				Name:     spec.External.Metric.Name,
				Selector: spec.External.Metric.Selector,
			},
			Current: autoscalingv2.MetricValueStatus{
				Value: &newVal,
			},
		}
	default:
		return fmt.Errorf("relative overrides are only supported for Resource and External metrics, not %q", spec.Type)
	}
	return nil
}

// resolveMetricSpec finds the spec metric matching the given name (case-insensitive).
func resolveMetricSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricSpec, bool) {
	lower := strings.ToLower(name)
	for _, m := range hpa.Spec.Metrics {
		switch m.Type {
		case autoscalingv2.ResourceMetricSourceType:
			if strings.ToLower(string(m.Resource.Name)) == lower {
				return m, true
			}
		case autoscalingv2.ExternalMetricSourceType:
			if strings.ToLower(m.External.Metric.Name) == lower {
				return m, true
			}
		case autoscalingv2.PodsMetricSourceType:
			if strings.ToLower(m.Pods.Metric.Name) == lower {
				return m, true
			}
		case autoscalingv2.ObjectMetricSourceType:
			if strings.ToLower(m.Object.Metric.Name) == lower {
				return m, true
			}
		case autoscalingv2.ContainerResourceMetricSourceType:
			if strings.ToLower(string(m.ContainerResource.Name)) == lower {
				return m, true
			}
		}
	}
	return autoscalingv2.MetricSpec{}, false
}

// findCurrentMetric returns the index of the current metric matching the given name.
func findCurrentMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (int, bool) {
	lower := strings.ToLower(name)
	for i, m := range hpa.Status.CurrentMetrics {
		if currentMetricNameMatches(m, lower) {
			return i, true
		}
	}
	return -1, false
}

// currentMetricNameMatches reports whether the given current-metric entry's
// name matches the already-lowercased target name.
func currentMetricNameMatches(m autoscalingv2.MetricStatus, lower string) bool {
	switch m.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return m.Resource != nil && strings.ToLower(string(m.Resource.Name)) == lower
	case autoscalingv2.ExternalMetricSourceType:
		return m.External != nil && strings.ToLower(m.External.Metric.Name) == lower
	case autoscalingv2.PodsMetricSourceType:
		return m.Pods != nil && strings.ToLower(m.Pods.Metric.Name) == lower
	case autoscalingv2.ObjectMetricSourceType:
		return m.Object != nil && strings.ToLower(m.Object.Metric.Name) == lower
	case autoscalingv2.ContainerResourceMetricSourceType:
		return m.ContainerResource != nil && strings.ToLower(string(m.ContainerResource.Name)) == lower
	default:
		return false
	}
}

// parseRelativeValue parses a relative change like +20% or -10% and applies it
// to the current int32 value, returning the new value.
func parseRelativeValue(value string, current int32) (int32, error) {
	if len(value) < 2 || !strings.HasSuffix(value, "%") {
		return 0, fmt.Errorf("invalid relative value %q: expected format like +20%% or -10%%", value)
	}
	pctStr := strings.TrimSuffix(value, "%")
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentage %q: %w", pctStr, err)
	}
	factor := 1.0 + pct/100.0
	result := math.Round(float64(current) * factor)
	if result < 0 {
		result = 0
	}
	return int32(result), nil
}

// parseRelativeQuantity applies a relative percentage change to a resource.Quantity.
func parseRelativeQuantity(value string, current *resource.Quantity) (resource.Quantity, error) {
	if len(value) < 2 || !strings.HasSuffix(value, "%") {
		return resource.Quantity{}, fmt.Errorf("invalid relative value %q: expected format like +20%% or -10%%", value)
	}
	pctStr := strings.TrimSuffix(value, "%")
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid percentage %q: %w", pctStr, err)
	}
	factor := 1.0 + pct/100.0
	newVal := int64(math.Round(float64(current.Value()) * factor))
	if newVal < 0 {
		newVal = 0
	}
	return *resource.NewQuantity(newVal, current.Format), nil
}

// computeProjectedReplicas returns ceil(currentReplicas * ratio) bounded by min/max.
func computeProjectedReplicas(currentReplicas int32, ratio float64, minReplicas, maxReplicas int32) int32 {
	projected := int32(math.Ceil(float64(currentReplicas) * ratio))
	if projected < minReplicas {
		return minReplicas
	}
	if projected > maxReplicas {
		return maxReplicas
	}
	return projected
}

// isResourceQuantity returns true if the value looks like a Kubernetes quantity
// (e.g., 4Gi, 500Mi, 2k) rather than a plain integer or percentage.
func isResourceQuantity(value string) bool {
	if strings.HasSuffix(value, "%") {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err != nil
}

// buildMetricSimulation creates a MetricSimulation for a single override.
func buildMetricSimulation(original, _ *autoscalingv2.HorizontalPodAutoscaler, name, value string, _, after SimulationState) MetricSimulation {
	ms := MetricSimulation{
		MetricName:        name,
		SimulatedValue:    value,
		ProjectedReplicas: after.DesiredReplicas,
	}

	// Find original value
	spec, specFound := resolveMetricSpec(original, name)
	if !specFound {
		ms.OriginalValue = "<not found>"
		return ms
	}

	idx, found := findCurrentMetric(original, name)
	if !found {
		ms.OriginalValue = "<no current value>"
		return ms
	}

	ms.OriginalValue = formatMetricValue(original.Status.CurrentMetrics[idx], spec.Type)

	// Compute projected ratio for resource utilization metrics
	if spec.Type == autoscalingv2.ResourceMetricSourceType && strings.HasSuffix(value, "%") {
		if updated := applyUtilizationPercentOverride(value, spec, original); updated != nil {
			updated.applyTo(&ms)
		}
	}

	// Compute projected ratio for relative changes
	if (strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-")) && spec.Type == autoscalingv2.ResourceMetricSourceType {
		currentUtil := original.Status.CurrentMetrics[idx].Resource.Current.AverageUtilization
		if currentUtil != nil {
			if updated := applyRelativeUtilizationOverride(value, *currentUtil, spec, original); updated != nil {
				updated.applyTo(&ms)
			}
		}
	}

	return ms
}

// metricSimulationUpdate holds the computed projection fields to apply to a
// MetricSimulation after a metric override is evaluated.
type metricSimulationUpdate struct {
	ProjectedRatio    *float64
	ProjectedReplicas int32
	SimulatedValue    string
}

func (u metricSimulationUpdate) applyTo(ms *MetricSimulation) {
	if u.ProjectedRatio != nil {
		ms.ProjectedRatio = u.ProjectedRatio
	}
	ms.ProjectedReplicas = u.ProjectedReplicas
	if u.SimulatedValue != "" {
		ms.SimulatedValue = u.SimulatedValue
	}
}

func applyUtilizationPercentOverride(value string, spec autoscalingv2.MetricSpec, original *autoscalingv2.HorizontalPodAutoscaler) *metricSimulationUpdate {
	pctStr := strings.TrimSuffix(value, "%")
	simUtil, err := strconv.ParseInt(pctStr, 10, 32)
	if err != nil {
		return nil
	}
	targetUtil := resolveTargetUtilization(spec)
	if targetUtil <= 0 {
		return nil
	}
	ratio := float64(simUtil) / float64(targetUtil)
	return &metricSimulationUpdate{
		ProjectedRatio:    &ratio,
		ProjectedReplicas: computeProjectedFromRatio(original, ratio),
	}
}

func applyRelativeUtilizationOverride(value string, currentUtil int32, spec autoscalingv2.MetricSpec, original *autoscalingv2.HorizontalPodAutoscaler) *metricSimulationUpdate {
	newUtil, err := parseRelativeValue(value, currentUtil)
	if err != nil {
		return nil
	}
	targetUtil := resolveTargetUtilization(spec)
	if targetUtil <= 0 {
		return nil
	}
	ratio := float64(newUtil) / float64(targetUtil)
	return &metricSimulationUpdate{
		ProjectedRatio:    &ratio,
		ProjectedReplicas: computeProjectedFromRatio(original, ratio),
		SimulatedValue:    fmt.Sprintf("%d%%", newUtil),
	}
}

func resolveTargetUtilization(spec autoscalingv2.MetricSpec) int32 {
	if spec.Resource.Target.AverageUtilization != nil {
		return *spec.Resource.Target.AverageUtilization
	}
	return 0
}

func computeProjectedFromRatio(original *autoscalingv2.HorizontalPodAutoscaler, ratio float64) int32 {
	minReplicas := int32(1)
	if original.Spec.MinReplicas != nil {
		minReplicas = *original.Spec.MinReplicas
	}
	return computeProjectedReplicas(
		original.Status.DesiredReplicas, ratio,
		minReplicas, original.Spec.MaxReplicas,
	)
}

// formatMetricValue returns a display string for a current metric value.
func formatMetricValue(metric autoscalingv2.MetricStatus, metricType autoscalingv2.MetricSourceType) string {
	switch metricType {
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource != nil {
			if metric.Resource.Current.AverageUtilization != nil {
				return fmt.Sprintf("%d%%", *metric.Resource.Current.AverageUtilization)
			}
			if metric.Resource.Current.AverageValue != nil {
				return metric.Resource.Current.AverageValue.String()
			}
		}
	case autoscalingv2.ExternalMetricSourceType:
		if metric.External != nil {
			if metric.External.Current.Value != nil {
				return metric.External.Current.Value.String()
			}
			if metric.External.Current.AverageValue != nil {
				return metric.External.Current.AverageValue.String()
			}
		}
	case autoscalingv2.PodsMetricSourceType:
		if metric.Pods != nil && metric.Pods.Current.AverageValue != nil {
			return metric.Pods.Current.AverageValue.String()
		}
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
