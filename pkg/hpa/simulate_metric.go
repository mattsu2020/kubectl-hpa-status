package hpa

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SimulateMetricChange simulates metric value changes and returns a
// SimulationResult with projected replica counts and impact analysis.
// The original HPA is not mutated; a deep copy is used internally.
func SimulateMetricChange(hpa *autoscalingv2.HorizontalPodAutoscaler, metricOverrides map[string]string, weights HealthWeights) (*SimulationResult, error) {
	return SimulateScenario(hpa, nil, metricOverrides, weights, SimulationExtendedOptions{})
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
		return applyResourceMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.ContainerResourceMetricSourceType:
		return applyContainerResourceMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.ExternalMetricSourceType:
		return applyExternalMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.PodsMetricSourceType:
		return applyPodsMetricOverride(hpa, spec, idx, value)
	case autoscalingv2.ObjectMetricSourceType:
		return applyObjectMetricOverride(hpa, spec, idx, value)
	default:
		return fmt.Errorf("unsupported metric type %q for metric %q", spec.Type, name)
	}
}

func applyContainerResourceMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	current := autoscalingv2.MetricValueStatus{}
	if spec.ContainerResource.Target.Type == autoscalingv2.UtilizationMetricType {
		parsed, err := strconv.ParseInt(strings.TrimSuffix(value, "%"), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid utilization value %q: %w", value, err)
		}
		utilization := int32(parsed)
		current.AverageUtilization = &utilization
	} else {
		quantity, err := parseMetricQuantity(value, "container resource")
		if err != nil {
			return err
		}
		current.AverageValue = &quantity
	}
	hpa.Status.CurrentMetrics[idx].ContainerResource = &autoscalingv2.ContainerResourceMetricStatus{
		Name: spec.ContainerResource.Name, Container: spec.ContainerResource.Container, Current: current,
	}
	return nil
}

// parseMetricQuantity parses a value string into a resource.Quantity, wrapping
// the parse error with metricType/name context.
func parseMetricQuantity(value, metricType string) (resource.Quantity, error) {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid %s metric quantity %q: %w", metricType, value, err)
	}
	return q, nil
}

// applyResourceMetricOverride preserves the value field selected by the
// metric target so ratio projection remains aligned with the spec.
func applyResourceMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	resName := spec.Resource.Name
	switch spec.Resource.Target.Type {
	case autoscalingv2.UtilizationMetricType:
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
	case autoscalingv2.AverageValueMetricType:
		q, err := parseMetricQuantity(value, "resource")
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name: resName,
			Current: autoscalingv2.MetricValueStatus{
				AverageValue: &q,
			},
		}
	default:
		return fmt.Errorf("unsupported resource metric target type %q", spec.Resource.Target.Type)
	}
	return nil
}

// applyExternalMetricOverride sets the current value of an External metric,
// choosing AverageValue vs Value from the spec target type.
func applyExternalMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	q, err := parseMetricQuantity(value, "external")
	if err != nil {
		return err
	}
	current := autoscalingv2.MetricValueStatus{}
	switch spec.External.Target.Type {
	case autoscalingv2.AverageValueMetricType:
		current.AverageValue = &q
	case autoscalingv2.ValueMetricType:
		current.Value = &q
	default:
		return fmt.Errorf("unsupported external metric target type %q", spec.External.Target.Type)
	}
	hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
		Metric:  spec.External.Metric,
		Current: current,
	}
	return nil
}

func applyObjectMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	quantity, err := parseMetricQuantity(value, "object")
	if err != nil {
		return err
	}
	current := autoscalingv2.MetricValueStatus{}
	if spec.Object.Target.Type == autoscalingv2.AverageValueMetricType || spec.Object.Target.AverageValue != nil {
		current.AverageValue = &quantity
	} else {
		current.Value = &quantity
	}
	hpa.Status.CurrentMetrics[idx].Object = &autoscalingv2.ObjectMetricStatus{
		Metric:          spec.Object.Metric,
		DescribedObject: spec.Object.DescribedObject,
		Current:         current,
	}
	return nil
}

// applyPodsMetricOverride sets the current AverageValue of a Pods metric.
func applyPodsMetricOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	q, err := parseMetricQuantity(value, "pods")
	if err != nil {
		return err
	}
	hpa.Status.CurrentMetrics[idx].Pods = &autoscalingv2.PodsMetricStatus{
		Metric: autoscalingv2.MetricIdentifier{
			Name:     spec.Pods.Metric.Name,
			Selector: spec.Pods.Metric.Selector,
		},
		Current: autoscalingv2.MetricValueStatus{
			AverageValue: &q,
		},
	}
	return nil
}

// applyRelativeOverride handles +/- percentage relative changes.
func applyRelativeOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return applyRelativeResourceOverride(hpa, spec, idx, value)
	case autoscalingv2.ExternalMetricSourceType:
		return applyRelativeExternalOverride(hpa, spec, idx, value)
	default:
		return fmt.Errorf("relative overrides are only supported for Resource and External metrics, not %q", spec.Type)
	}
}

func applyRelativeResourceOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	current := hpa.Status.CurrentMetrics[idx].Resource
	if current == nil {
		return fmt.Errorf("cannot apply relative change: no current value for metric %q", spec.Resource.Name)
	}
	switch spec.Resource.Target.Type {
	case autoscalingv2.UtilizationMetricType:
		if current.Current.AverageUtilization == nil {
			return fmt.Errorf("cannot apply relative change: no current utilization for metric %q", spec.Resource.Name)
		}
		newValue, err := parseRelativeValue(value, *current.Current.AverageUtilization)
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name:    spec.Resource.Name,
			Current: autoscalingv2.MetricValueStatus{AverageUtilization: &newValue},
		}
	case autoscalingv2.AverageValueMetricType:
		if current.Current.AverageValue == nil {
			return fmt.Errorf("cannot apply relative change: no current average value for metric %q", spec.Resource.Name)
		}
		newValue, err := parseRelativeQuantity(value, current.Current.AverageValue)
		if err != nil {
			return err
		}
		hpa.Status.CurrentMetrics[idx].Resource = &autoscalingv2.ResourceMetricStatus{
			Name:    spec.Resource.Name,
			Current: autoscalingv2.MetricValueStatus{AverageValue: &newValue},
		}
	default:
		return fmt.Errorf("unsupported resource metric target type %q", spec.Resource.Target.Type)
	}
	return nil
}

func applyRelativeExternalOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec, idx int, value string) error {
	current := hpa.Status.CurrentMetrics[idx].External
	if current == nil {
		return fmt.Errorf("cannot apply relative change: no current value for external metric %q", spec.External.Metric.Name)
	}
	var currentQuantity *resource.Quantity
	switch spec.External.Target.Type {
	case autoscalingv2.AverageValueMetricType:
		currentQuantity = current.Current.AverageValue
	case autoscalingv2.ValueMetricType:
		currentQuantity = current.Current.Value
	default:
		return fmt.Errorf("unsupported external metric target type %q", spec.External.Target.Type)
	}
	if currentQuantity == nil {
		return fmt.Errorf("cannot apply relative change: current value shape does not match %q target for external metric %q", spec.External.Target.Type, spec.External.Metric.Name)
	}
	newValue, err := parseRelativeQuantity(value, currentQuantity)
	if err != nil {
		return err
	}
	next := autoscalingv2.MetricValueStatus{}
	if spec.External.Target.Type == autoscalingv2.AverageValueMetricType {
		next.AverageValue = &newValue
	} else {
		next.Value = &newValue
	}
	hpa.Status.CurrentMetrics[idx].External = &autoscalingv2.ExternalMetricStatus{
		Metric:  spec.External.Metric,
		Current: next,
	}
	return nil
}

// resolveMetricSpec finds one unambiguous spec metric matching the given name
// (case-insensitive). Ambiguous name-only references deliberately fail closed.
func resolveMetricSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricSpec, bool) {
	spec, err := resolveMetricSpecUnique(hpa, name)
	if err != nil {
		return autoscalingv2.MetricSpec{}, false
	}
	return spec, true
}

func resolveMetricSpecUnique(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (autoscalingv2.MetricSpec, error) {
	index, err := resolveMetricSpecIndexUnique(hpa, name)
	if err != nil {
		return autoscalingv2.MetricSpec{}, err
	}
	return hpa.Spec.Metrics[index], nil
}

func resolveMetricSpecIndexUnique(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (int, error) {
	if hpa == nil {
		return -1, ErrNilHPA
	}
	matches := make([]int, 0, 1)
	for i, metric := range hpa.Spec.Metrics {
		if metricSpecNameMatches(metric, name) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, ErrMetricNotFound
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("%w: %q matches %d metrics; use a unique metric name or remove duplicate selector/container variants", ErrMetricAmbiguous, name, len(matches))
	}
}

func findCurrentMetricForSpec(hpa *autoscalingv2.HorizontalPodAutoscaler, spec autoscalingv2.MetricSpec) (int, error) {
	if hpa == nil {
		return -1, ErrNilHPA
	}
	specID, err := MetricIDFromSpec(spec)
	if err != nil {
		return -1, err
	}
	match := -1
	for i, current := range hpa.Status.CurrentMetrics {
		currentID, currentErr := MetricIDFromStatus(current)
		if currentErr != nil || currentID != specID {
			continue
		}
		if match >= 0 {
			return -1, fmt.Errorf("%w: current status contains duplicate identity %#v", ErrMetricAmbiguous, specID)
		}
		match = i
	}
	return match, nil
}

// findCurrentMetric returns the canonical current metric for one unambiguous
// spec name. It fails closed when multiple spec or status identities match.
func findCurrentMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) (int, bool) {
	spec, err := resolveMetricSpecUnique(hpa, name)
	if err != nil {
		return -1, false
	}
	index, err := findCurrentMetricForSpec(hpa, spec)
	if err != nil || index < 0 {
		return -1, false
	}
	return index, true
}

// parseRelativeValue parses a relative change like +20% or -10% and applies it
// to the current int32 value, returning the new value.
func parseRelativeValue(value string, current int32) (int32, error) {
	pct, err := parseRelativePercentage(value)
	if err != nil {
		return 0, err
	}
	factor := 1.0 + pct/100.0
	result := math.Round(float64(current) * factor)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("relative value %q produces a non-finite result", value)
	}
	if result < float64(math.MinInt32) || result > float64(math.MaxInt32) {
		return 0, fmt.Errorf("relative value %q produces a result outside the int32 range", value)
	}
	if result < 0 {
		result = 0
	}
	return int32(result), nil
}

// parseRelativeQuantity applies a relative percentage change to a resource.Quantity.
func parseRelativeQuantity(value string, current *resource.Quantity) (resource.Quantity, error) {
	if current == nil {
		return resource.Quantity{}, fmt.Errorf("cannot apply relative value %q to a nil quantity", value)
	}
	if current.Sign() < 0 {
		return resource.Quantity{}, fmt.Errorf("cannot apply relative value %q to a negative quantity", value)
	}
	maxMilliQuantity := resource.NewMilliQuantity(math.MaxInt64, resource.DecimalSI)
	if current.Cmp(*maxMilliQuantity) > 0 {
		return resource.Quantity{}, fmt.Errorf("current quantity is outside the supported int64 milli-unit range")
	}
	pct, err := parseRelativePercentage(value)
	if err != nil {
		return resource.Quantity{}, err
	}
	if pct == 0 {
		return current.DeepCopy(), nil
	}
	factor := 1.0 + pct/100.0
	result := math.Round(float64(current.MilliValue()) * factor)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return resource.Quantity{}, fmt.Errorf("relative value %q produces a non-finite quantity", value)
	}
	// float64(math.MaxInt64) rounds to 1<<63, which is already outside the
	// positive int64 range. Treat that boundary conservatively as overflow.
	if result < float64(math.MinInt64) || result >= float64(math.MaxInt64) {
		return resource.Quantity{}, fmt.Errorf("relative value %q produces a quantity outside the int64 range", value)
	}
	newMilliValue := int64(result)
	if newMilliValue < 0 {
		newMilliValue = 0
	}
	return *resource.NewMilliQuantity(newMilliValue, current.Format), nil
}

func parseRelativePercentage(value string) (float64, error) {
	if len(value) < 2 || !strings.HasSuffix(value, "%") {
		return 0, fmt.Errorf("invalid relative value %q: expected format like +20%% or -10%%", value)
	}
	pctText := strings.TrimSuffix(value, "%")
	pct, err := strconv.ParseFloat(pctText, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentage %q: %w", pctText, err)
	}
	if math.IsNaN(pct) || math.IsInf(pct, 0) {
		return 0, fmt.Errorf("invalid percentage %q: value must be finite", pctText)
	}
	factor := 1.0 + pct/100.0
	if math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, fmt.Errorf("invalid percentage %q: relative factor must be finite", pctText)
	}
	return pct, nil
}

// computeProjectedReplicas returns ceil(currentReplicas * ratio) bounded by min/max.
func computeProjectedReplicas(currentReplicas int32, ratio float64, minReplicas, maxReplicas int32) int32 {
	projected, usable := util.ProjectedReplicasForRatio(currentReplicas, ratio)
	if !usable {
		return currentReplicas
	}
	if projected < minReplicas {
		return minReplicas
	}
	if projected > maxReplicas {
		return maxReplicas
	}
	return projected
}

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
	_, ratio := metricImpactRatio(modified, modified.Status.CurrentMetrics[modifiedIdx])
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
		within, tolerance := ratioWithinTolerance(modified, *ratio)
		if within {
			ms.ToleranceImpact = fmt.Sprintf("%s tolerance %.3f suppresses scaling", toleranceDirection(*ratio), tolerance)
		} else {
			ms.ToleranceImpact = fmt.Sprintf("outside %s tolerance %.3f", toleranceDirection(*ratio), tolerance)
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
		return FormatMetricValueStatus(*value)
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
