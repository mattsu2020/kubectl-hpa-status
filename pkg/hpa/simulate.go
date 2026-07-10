package hpa

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
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

// applySimulationOverride modifies a single field on the HPA spec using dot-notation path.
func applySimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, path, value string) error {
	normalizedPath := normalizeSimulationPath(path)
	if strings.HasPrefix(normalizedPath, "metric.") && strings.HasSuffix(normalizedPath, ".target") {
		name := strings.TrimSuffix(strings.TrimPrefix(normalizedPath, "metric."), ".target")
		return applyMetricTargetOverride(hpa, name, value)
	}
	if strings.HasSuffix(normalizedPath, ".targetaverageutilization") {
		name := strings.TrimSuffix(normalizedPath, ".targetaverageutilization")
		return applyMetricTargetOverride(hpa, name, value)
	}

	if handled, err := applyReplicaSimulationOverride(hpa, normalizedPath, value); handled {
		return err
	}
	if handled, err := applyBehaviorSimulationOverride(hpa, normalizedPath, value); handled {
		return err
	}
	switch normalizedPath {
	case "tolerance":
		return applyToleranceOverride(hpa, "both", value)
	case "scaleup.tolerance":
		return applyToleranceOverride(hpa, "up", value)
	case "scaledown.tolerance":
		return applyToleranceOverride(hpa, "down", value)
	default:
		return fmt.Errorf("unsupported path %q; supported: maxReplicas, minReplicas, scaleDown.stabilizationWindowSeconds, scaleDown.stabilizationWindow, scaleUp.stabilizationWindowSeconds, scaleUp.stabilizationWindow, scaleDown.selectPolicy, scaleUp.selectPolicy, targetAverageUtilization, metric.<name>.target, tolerance, scaleUp.tolerance, scaleDown.tolerance", path)
	}
}

func applyReplicaSimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, normalizedPath, value string) (bool, error) {
	switch normalizedPath {
	case "maxreplicas":
		v, err := parseNonNegativeInt32(value, 1, "maxReplicas must be >= 1")
		if err != nil {
			return true, err
		}
		hpa.Spec.MaxReplicas = v
		return true, nil
	case "minreplicas":
		v, err := parseNonNegativeInt32(value, 0, "minReplicas must be >= 0")
		if err != nil {
			return true, err
		}
		hpa.Spec.MinReplicas = &v
		return true, nil
	case "targetaverageutilization":
		v, err := parsePositiveInt32(value)
		if err != nil {
			return true, err
		}
		applyAverageUtilizationToResourceMetrics(hpa, v)
		return true, nil
	default:
		return false, nil
	}
}

func applyBehaviorSimulationOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, normalizedPath, value string) (bool, error) {
	switch normalizedPath {
	case "scaledown.stabilizationwindowseconds":
		v, err := parseNonNegativeInt32(value, 0, "stabilizationWindowSeconds must be >= 0")
		if err != nil {
			return true, err
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds = &v
		return true, nil
	case "scaleup.stabilizationwindowseconds":
		v, err := parseNonNegativeInt32(value, 0, "stabilizationWindowSeconds must be >= 0")
		if err != nil {
			return true, err
		}
		ensureBehavior(hpa)
		hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds = &v
		return true, nil
	case "scaledown.selectpolicy":
		ensureBehavior(hpa)
		p := selectPolicy(value)
		hpa.Spec.Behavior.ScaleDown.SelectPolicy = &p
		return true, nil
	case "scaleup.selectpolicy":
		ensureBehavior(hpa)
		p := selectPolicy(value)
		hpa.Spec.Behavior.ScaleUp.SelectPolicy = &p
		return true, nil
	default:
		return false, nil
	}
}

func applyToleranceOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, direction, value string) error {
	tolerance, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("invalid tolerance %q: %w", value, err)
	}
	floatValue := tolerance.AsApproximateFloat64()
	if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || floatValue < 0 || floatValue > 1 {
		return fmt.Errorf("tolerance must be between 0 and 1, got %q", value)
	}
	ensureBehavior(hpa)
	if direction == "both" || direction == "up" {
		up := tolerance.DeepCopy()
		hpa.Spec.Behavior.ScaleUp.Tolerance = &up
	}
	if direction == "both" || direction == "down" {
		down := tolerance.DeepCopy()
		hpa.Spec.Behavior.ScaleDown.Tolerance = &down
	}
	return nil
}

func applyMetricTargetOverride(hpa *autoscalingv2.HorizontalPodAutoscaler, name, value string) error {
	for i := range hpa.Spec.Metrics {
		spec := &hpa.Spec.Metrics[i]
		if !metricSpecNameMatches(*spec, name) {
			continue
		}
		target := metricTargetPointer(spec)
		if target == nil {
			return fmt.Errorf("metric %q has no target", name)
		}
		if target.Type == autoscalingv2.UtilizationMetricType || target.AverageUtilization != nil {
			parsed := strings.TrimSuffix(value, "%")
			utilization, err := parsePositiveInt32(parsed)
			if err != nil {
				return fmt.Errorf("invalid utilization target %q for metric %q: %w", value, name, err)
			}
			target.Type = autoscalingv2.UtilizationMetricType
			target.AverageUtilization = &utilization
			target.AverageValue = nil
			target.Value = nil
			return nil
		}

		quantity, err := resource.ParseQuantity(value)
		if err != nil || quantity.Sign() <= 0 {
			if err == nil {
				err = errors.New("target must be greater than zero")
			}
			return fmt.Errorf("invalid quantity target %q for metric %q: %w", value, name, err)
		}
		if target.Type == autoscalingv2.ValueMetricType || target.Value != nil {
			target.Type = autoscalingv2.ValueMetricType
			target.Value = &quantity
			target.AverageValue = nil
		} else {
			target.Type = autoscalingv2.AverageValueMetricType
			target.AverageValue = &quantity
			target.Value = nil
		}
		target.AverageUtilization = nil
		return nil
	}
	return fmt.Errorf("metric %q: %w", name, ErrMetricNotFound)
}

func metricSpecNameMatches(spec autoscalingv2.MetricSpec, name string) bool {
	wanted := strings.ToLower(strings.TrimSpace(name))
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return spec.Resource != nil && strings.ToLower(string(spec.Resource.Name)) == wanted
	case autoscalingv2.ContainerResourceMetricSourceType:
		return spec.ContainerResource != nil && strings.ToLower(string(spec.ContainerResource.Name)) == wanted
	case autoscalingv2.PodsMetricSourceType:
		return spec.Pods != nil && strings.ToLower(spec.Pods.Metric.Name) == wanted
	case autoscalingv2.ObjectMetricSourceType:
		return spec.Object != nil && strings.ToLower(spec.Object.Metric.Name) == wanted
	case autoscalingv2.ExternalMetricSourceType:
		return spec.External != nil && strings.ToLower(spec.External.Metric.Name) == wanted
	default:
		return false
	}
}

func metricTargetPointer(spec *autoscalingv2.MetricSpec) *autoscalingv2.MetricTarget {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if spec.Resource != nil {
			return &spec.Resource.Target
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if spec.ContainerResource != nil {
			return &spec.ContainerResource.Target
		}
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods != nil {
			return &spec.Pods.Target
		}
	case autoscalingv2.ObjectMetricSourceType:
		if spec.Object != nil {
			return &spec.Object.Target
		}
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External != nil {
			return &spec.External.Target
		}
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
	case "scaleup.tolerance":
		return originalDirectionalTolerance(hpa, true)
	case "scaledown.tolerance":
		return originalDirectionalTolerance(hpa, false)
	case "tolerance":
		up, down := effectiveDirectionalTolerances(hpa)
		return fmt.Sprintf("scaleUp=%.3g,scaleDown=%.3g", up, down)
	default:
		if strings.HasPrefix(normalizeSimulationPath(path), "metric.") && strings.HasSuffix(normalizeSimulationPath(path), ".target") {
			name := strings.TrimSuffix(strings.TrimPrefix(normalizeSimulationPath(path), "metric."), ".target")
			if spec, found := resolveMetricSpec(hpa, name); found {
				if target := metricTargetPointer(&spec); target != nil {
					return FormatMetricTarget(*target)
				}
			}
		}
		return "<unknown>"
	}
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
