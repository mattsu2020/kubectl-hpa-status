package hpa

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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
	// ErrUnsupportedSimulationSemantics is returned when a field cannot be
	// projected accurately from a single HPA status snapshot.
	ErrUnsupportedSimulationSemantics = errors.New("simulation requires unavailable controller history")
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
	result.RiskWarnings = assessExtendedRisk(modified, overrides, result)

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
	if err := validateSimulatedHPA(modified); err != nil {
		return nil, err
	}
	if len(overrides) > 0 || len(metricOverrides) > 0 {
		if err := validateSimulatedZeroProjection(modified); err != nil {
			return nil, err
		}
		recomputeSimulatedDesired(modified)
	}
	return modified, nil
}

func recomputeSimulatedDesired(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	scaledToZero := simulatedConditionTrue(hpa, autoscalingv2.ScaledToZero)
	if hpa.Status.CurrentReplicas == 0 &&
		!shouldComputeSimulatedMetricsFromZero(hpa, minReplicas, scaledToZero) {
		hpa.Status.DesiredReplicas = 0
		replaceSimulatedScalingActive(hpa, corev1.ConditionFalse, "ScalingDisabled",
			"scaling is disabled because the target was manually scaled to zero")
		replaceSimulatedControllerConditions(hpa, false, "DesiredWithinRange")
		return
	}

	desired, found := simulatedDesiredFromMetrics(hpa)
	if !found {
		desired = hpa.Status.DesiredReplicas
	}
	// Missing metrics conservatively block a scale-down in the public estimate.
	if found && len(hpa.Status.CurrentMetrics) < len(hpa.Spec.Metrics) && desired < hpa.Status.CurrentReplicas {
		desired = hpa.Status.CurrentReplicas
	}

	desired, limited, limitedReason := normalizeSimulatedDesired(
		hpa,
		desired,
		minReplicas,
		hpa.Spec.MaxReplicas,
	)
	if hpa.Status.CurrentReplicas == 0 && minReplicas != 0 && scaledToZero && desired < minReplicas {
		desired = minReplicas
	}
	hpa.Status.DesiredReplicas = desired
	if hpa.Status.CurrentReplicas == 0 && scaledToZero && found {
		replaceSimulatedScalingActive(hpa, corev1.ConditionTrue, "ValidMetricFound",
			"the projected replica count was calculated from visible metric data")
	}
	replaceSimulatedControllerConditions(hpa, limited, limitedReason)
}

func simulatedDesiredFromMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) (int32, bool) {
	var desired int32
	found := false
	for _, metric := range hpa.Status.CurrentMetrics {
		_, ratio := metricImpactRatio(hpa, metric)
		if ratio == nil || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) || *ratio < 0 {
			continue
		}
		metricDesired, projectable := estimatedSimulatedMetricDesired(hpa, metric, *ratio)
		if !projectable {
			continue
		}
		if !found || metricDesired > desired {
			desired = metricDesired
			found = true
		}
	}
	return desired, found
}

func estimatedSimulatedMetricDesired(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	metric autoscalingv2.MetricStatus,
	ratio float64,
) (int32, bool) {
	if hpa.Status.CurrentReplicas != 0 {
		return estimatedDesiredForRatio(hpa, ratio), true
	}

	// At zero replicas, the controller's Object/External Value algorithm uses
	// ceil(currentValue/targetValue), which is exactly ceil(ratio), and skips
	// tolerance. Per-pod targets and pod/resource metrics need pod state that is
	// unavailable at zero and are intentionally not guessed.
	if metric.Type != autoscalingv2.ObjectMetricSourceType &&
		metric.Type != autoscalingv2.ExternalMetricSourceType {
		return 0, false
	}
	target, ok := matchingMetricTarget(hpa, metric)
	if !ok || target.Type != autoscalingv2.ValueMetricType || target.Value == nil {
		return 0, false
	}
	projected := math.Ceil(ratio)
	if projected > math.MaxInt32 {
		return math.MaxInt32, true
	}
	return int32(projected), true
}

func validateSimulatedZeroProjection(hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	if hpa.Status.CurrentReplicas != 0 {
		return nil
	}
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	scaledToZero := simulatedConditionTrue(hpa, autoscalingv2.ScaledToZero)
	if !shouldComputeSimulatedMetricsFromZero(hpa, minReplicas, scaledToZero) {
		return nil
	}
	for _, metric := range hpa.Status.CurrentMetrics {
		_, ratio := metricImpactRatio(hpa, metric)
		if ratio == nil || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) || *ratio < 0 {
			continue
		}
		if _, ok := estimatedSimulatedMetricDesired(hpa, metric, *ratio); ok {
			return nil
		}
	}
	return fmt.Errorf("%w: scale-from-zero projection requires a visible Object or External Value metric",
		ErrUnsupportedSimulationSemantics)
}

func shouldComputeSimulatedMetricsFromZero(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	minReplicas int32,
	scaledToZero bool,
) bool {
	if !scaledToZero {
		return false
	}
	return minReplicas != 0 || hasSimulatedObjectOrExternalMetric(hpa)
}

func hasSimulatedObjectOrExternalMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ObjectMetricSourceType ||
			metric.Type == autoscalingv2.ExternalMetricSourceType {
			return true
		}
	}
	return false
}

func simulatedConditionTrue(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	conditionType autoscalingv2.HorizontalPodAutoscalerConditionType,
) bool {
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func replaceSimulatedScalingActive(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	status corev1.ConditionStatus,
	reason, message string,
) {
	conditions := make([]autoscalingv2.HorizontalPodAutoscalerCondition, 0, len(hpa.Status.Conditions)+1)
	for _, condition := range hpa.Status.Conditions {
		if condition.Type != autoscalingv2.ScalingActive {
			conditions = append(conditions, condition)
		}
	}
	conditions = append(conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:    autoscalingv2.ScalingActive,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	hpa.Status.Conditions = conditions
}

// normalizeSimulatedDesired mirrors the HPA controller's ordering when it
// combines min/max bounds with scaling-rate policies. In particular, a target
// that is already outside its configured range is moved toward the range at the
// permitted rate rather than being clamped past that rate in one step.
//
// Scaling-event history is not present in an HPA object. Policy limits therefore
// use the current replica count as the beginning-of-period baseline.
func normalizeSimulatedDesired(
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	desired, minReplicas, maxReplicas int32,
) (int32, bool, string) {
	current := hpa.Status.CurrentReplicas

	// A nil behavior uses the controller's legacy normalization path. Its
	// scale-up ceiling is max(2*currentReplicas, 4); scale-down has no rate
	// ceiling beyond minReplicas.
	if hpa.Spec.Behavior == nil {
		maximumAllowed := maxReplicas
		reason := "TooManyReplicas"
		scaleUpLimit := legacySimulatedScaleUpLimit(current)
		if maximumAllowed > scaleUpLimit {
			maximumAllowed = scaleUpLimit
			reason = "ScaleUpLimit"
		}
		switch {
		case desired < minReplicas:
			return minReplicas, true, "TooFewReplicas"
		case desired > maximumAllowed:
			return maximumAllowed, true, reason
		default:
			return desired, false, "DesiredWithinRange"
		}
	}

	if desired > current {
		scaleUpLimit := simulatedPolicyReplicaLimit(current, true, hpa.Spec.Behavior.ScaleUp)
		if scaleUpLimit < current {
			scaleUpLimit = current
		}
		maximumAllowed := maxReplicas
		reason := "TooManyReplicas"
		if maximumAllowed > scaleUpLimit {
			maximumAllowed = scaleUpLimit
			reason = "ScaleUpLimit"
		}
		if desired > maximumAllowed {
			return maximumAllowed, true, reason
		}
		return desired, false, "DesiredWithinRange"
	}

	if desired < current {
		scaleDownLimit := simulatedPolicyReplicaLimit(current, false, hpa.Spec.Behavior.ScaleDown)
		if scaleDownLimit > current {
			scaleDownLimit = current
		}
		minimumAllowed := minReplicas
		reason := "TooFewReplicas"
		if minimumAllowed < scaleDownLimit {
			minimumAllowed = scaleDownLimit
			reason = "ScaleDownLimit"
		}
		if desired < minimumAllowed {
			return minimumAllowed, true, reason
		}
		return desired, false, "DesiredWithinRange"
	}

	return desired, false, "DesiredWithinRange"
}

func legacySimulatedScaleUpLimit(current int32) int32 {
	limit := max(int64(current)*2, int64(4))
	if limit > int64(math.MaxInt32) {
		return math.MaxInt32
	}
	return int32(limit)
}

// simulatedPolicyReplicaLimit applies the immediate per-period policy ceiling
// using the current replica count as the period baseline. Nil policies are
// expanded to the autoscaling/v2 API defaults, matching API defaulting after a
// local behavior override introduces a previously absent behavior block.
func simulatedPolicyReplicaLimit(current int32, scaleUp bool, rules *autoscalingv2.HPAScalingRules) int32 {
	policy := autoscalingv2.MaxChangePolicySelect
	if rules != nil && rules.SelectPolicy != nil {
		policy = *rules.SelectPolicy
	}
	if policy == autoscalingv2.DisabledPolicySelect {
		return current
	}

	policies := defaultSimulatedScalingPolicies(scaleUp)
	if rules != nil && rules.Policies != nil {
		policies = rules.Policies
	}

	limit, ok := selectedPolicyReplicaLimit(current, scaleUp, policy, policies)
	if !ok {
		// An explicitly empty policy list is invalid in the Kubernetes API. Be
		// conservative if such an object is nevertheless supplied directly.
		return current
	}
	return limit
}

func defaultSimulatedScalingPolicies(scaleUp bool) []autoscalingv2.HPAScalingPolicy {
	if !scaleUp {
		return []autoscalingv2.HPAScalingPolicy{{
			Type:          autoscalingv2.PercentScalingPolicy,
			Value:         100,
			PeriodSeconds: 15,
		}}
	}
	return []autoscalingv2.HPAScalingPolicy{
		{
			Type:          autoscalingv2.PodsScalingPolicy,
			Value:         4,
			PeriodSeconds: 15,
		},
		{
			Type:          autoscalingv2.PercentScalingPolicy,
			Value:         100,
			PeriodSeconds: 15,
		},
	}
}

func selectedPolicyReplicaLimit(current int32, scaleUp bool, selectPolicy autoscalingv2.ScalingPolicySelect, policies []autoscalingv2.HPAScalingPolicy) (int32, bool) {
	var selected int32
	found := false
	for _, policy := range policies {
		candidate, ok := policyReplicaLimit(current, scaleUp, policy)
		if !ok {
			continue
		}
		if !found {
			selected = candidate
			found = true
			continue
		}
		if scaleUp {
			if selectPolicy == autoscalingv2.MinChangePolicySelect {
				selected = min(selected, candidate)
			} else {
				selected = max(selected, candidate)
			}
			continue
		}
		if selectPolicy == autoscalingv2.MinChangePolicySelect {
			selected = max(selected, candidate)
		} else {
			selected = min(selected, candidate)
		}
	}
	return selected, found
}

func policyReplicaLimit(current int32, scaleUp bool, policy autoscalingv2.HPAScalingPolicy) (int32, bool) {
	if policy.Value <= 0 {
		return 0, false
	}

	current64 := int64(current)
	var candidate int64
	switch policy.Type {
	case autoscalingv2.PodsScalingPolicy:
		if scaleUp {
			candidate = current64 + int64(policy.Value)
		} else {
			candidate = current64 - int64(policy.Value)
		}
	case autoscalingv2.PercentScalingPolicy:
		if scaleUp {
			candidate = int64(math.Ceil(float64(current) * (1 + float64(policy.Value)/100)))
		} else {
			candidate = int64(float64(current) * (1 - float64(policy.Value)/100))
		}
	default:
		return 0, false
	}

	const maxInt32Value = int64(1<<31 - 1)
	if candidate > maxInt32Value {
		candidate = maxInt32Value
	}
	if candidate < 0 {
		candidate = 0
	}
	return int32(candidate), true
}

// replaceSimulatedControllerConditions removes controller observations that
// describe the live recommendation and replaces ScalingLimited with a condition
// derived from the projected recommendation.
func replaceSimulatedControllerConditions(hpa *autoscalingv2.HorizontalPodAutoscaler, limited bool, reason string) {
	conditions := make([]autoscalingv2.HorizontalPodAutoscalerCondition, 0, len(hpa.Status.Conditions)+1)
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == autoscalingv2.ScalingLimited || condition.Type == autoscalingv2.ScaledToZero {
			continue
		}
		if condition.Type == autoscalingv2.AbleToScale &&
			(condition.Reason == "ScaleUpStabilized" || condition.Reason == "ScaleDownStabilized") {
			continue
		}
		conditions = append(conditions, condition)
	}

	status := corev1.ConditionFalse
	message := "the projected desired replica count is within the simulated limits"
	if limited {
		status = corev1.ConditionTrue
		message = "the projected desired replica count is constrained by the simulated limits"
	}
	conditions = append(conditions, autoscalingv2.HorizontalPodAutoscalerCondition{
		Type:    autoscalingv2.ScalingLimited,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	hpa.Status.Conditions = conditions
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
