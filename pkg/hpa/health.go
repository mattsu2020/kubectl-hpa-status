package hpa

import (
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// Health computes the health state and score using default penalty weights.
func Health(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) (string, int) {
	result := HealthWithWeights(hpa, minReplicas, HealthWeights{})
	return string(result.State), result.Score
}

// HealthAccumulator centralizes health score updates so that all penalties
// (condition-based and enrichment-based) flow through a single mechanism.
// This prevents score/signal drift and makes penalty application auditable.
type HealthAccumulator struct {
	result HealthResult
}

// NewHealthAccumulator creates an accumulator starting at the given base score.
func NewHealthAccumulator(baseScore int) *HealthAccumulator {
	return &HealthAccumulator{
		result: HealthResult{Score: baseScore},
	}
}

// AddPenalty records a health penalty with reason and severity.
func (h *HealthAccumulator) AddPenalty(reason string, penalty int, severity HealthState) {
	h.result.Score -= penalty
	h.result.Signals = append(h.result.Signals, HealthSignal{
		Reason:   reason,
		Penalty:  penalty,
		Severity: severity,
	})
}

// SetState overrides the health state classification.
func (h *HealthAccumulator) SetState(state HealthState) {
	h.result.State = state
}

// Result returns a copy of the accumulated health result.
func (h *HealthAccumulator) Result() HealthResult {
	// Return a copy to preserve immutability
	signals := make([]HealthSignal, len(h.result.Signals))
	copy(signals, h.result.Signals)
	return HealthResult{
		State:   h.result.State,
		Score:   h.result.Score,
		Signals: signals,
	}
}

// hasCondition reports whether the HPA has a condition with the given type and status.
func hasCondition(conditions []autoscalingv2.HorizontalPodAutoscalerCondition, conditionType string, status corev1.ConditionStatus) bool {
	for _, c := range conditions {
		if string(c.Type) == conditionType && c.Status == status {
			return true
		}
	}
	return false
}

// hasMetricAboveTarget reports whether any current metric has a ratio above 1.0,
// indicating visible scaling pressure.
func hasMetricAboveTarget(currentMetrics []autoscalingv2.MetricStatus, hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range currentMetrics {
		_, ratio := metricImpactRatio(hpa, metric)
		if ratio != nil && *ratio > 1.0 {
			return true
		}
	}
	return false
}

// HealthWithWeights computes the typed health result using configurable penalty weights.
// Each penalty applied is recorded as a HealthSignal for transparency.
func HealthWithWeights(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, weights HealthWeights) HealthResult {
	if hpa == nil {
		return HealthResult{State: HealthError, Score: 0}
	}
	w := resolveWeights(weights)

	acc := NewHealthAccumulator(healthScoreMax)
	health := HealthOK

	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == "ScalingActive" && condition.Status != corev1.ConditionTrue:
			acc.AddPenalty("ScalingActive is not True", w.scalingInactive, HealthError)
			health = HealthError
		case condition.Type == "AbleToScale" && condition.Status != corev1.ConditionTrue:
			acc.AddPenalty("AbleToScale is not True", w.unableToScale, HealthError)
			health = HealthError
		case condition.Type == "ScalingLimited" && condition.Status == corev1.ConditionTrue:
			acc.AddPenalty("ScalingLimited is True", w.scalingLimited, HealthLimited)
			if health != HealthError {
				health = HealthLimited
			}
		case condition.Type == "AbleToScale" && condition.Reason == "ScaleDownStabilized":
			acc.AddPenalty("ScaleDownStabilized", w.scaleDownStabilized, HealthStabilized)
			if health == HealthOK {
				health = HealthStabilized
			}
		}
	}
	if hpa.Status.CurrentReplicas == hpa.Status.DesiredReplicas && hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas {
		hasLimited := hasCondition(hpa.Status.Conditions, "ScalingLimited", corev1.ConditionTrue)
		hasPressure := hasMetricAboveTarget(hpa.Status.CurrentMetrics, hpa)
		if hasLimited || hasPressure {
			acc.AddPenalty("Implicit maxReplicas ceiling (current==desired==max with pressure)", w.implicitMaxReplicas, HealthLimited)
			if health == HealthOK {
				health = HealthLimited
			}
		}
	}
	if hpa.Status.DesiredReplicas == minReplicas && hasCondition(hpa.Status.Conditions, "ScalingLimited", corev1.ConditionTrue) {
		acc.AddPenalty("At minimum replicas with ScalingLimited", w.atMinimumReplicas, health)
	}
	acc.SetState(health)
	result := acc.Result()
	if result.Score < 0 {
		result.Score = 0
	}
	return result
}

// resolvedWeights is the internal resolved form of HealthWeights where all
// nil pointers have been replaced with default penalty values.
type resolvedWeights struct {
	scalingInactive     int
	unableToScale       int
	scalingLimited      int
	implicitMaxReplicas int
	scaleDownStabilized int
	atMinimumReplicas   int
	kedaInactiveTrigger int
	vpaConflict         int
}

func resolveWeights(w HealthWeights) resolvedWeights {
	return resolvedWeights{
		scalingInactive:     weightOr(w.ScalingInactive, healthPenaltyScalingInactive),
		unableToScale:       weightOr(w.UnableToScale, healthPenaltyUnableToScale),
		scalingLimited:      weightOr(w.ScalingLimited, healthPenaltyScalingLimited),
		implicitMaxReplicas: weightOr(w.ImplicitMaxReplicas, healthPenaltyImplicitMaxReplicas),
		scaleDownStabilized: weightOr(w.ScaleDownStabilized, healthPenaltyScaleDownStabilized),
		atMinimumReplicas:   weightOr(w.AtMinimumReplicas, healthPenaltyAtMinimumReplicas),
		kedaInactiveTrigger: weightOr(w.KEDAInactiveTrigger, healthPenaltyKEDAInactiveTrigger),
		vpaConflict:         weightOr(w.VPAConflict, healthPenaltyVPAConflict),
	}
}

// weightOr returns the pointed-to value, or the default if nil.
func weightOr(w *int, defaultVal int) int {
	if w != nil {
		return *w
	}
	return defaultVal
}

// ApplyEnrichmentPenalties adjusts the health score and state based on
// KEDA and VPA enrichment data populated after AnalyzeWithOptions.
func ApplyEnrichmentPenalties(a *Analysis, weights HealthWeights) {
	if a == nil {
		return
	}
	w := resolveWeights(weights)

	acc := NewHealthAccumulator(a.HealthScore)
	if a.HealthResult != nil {
		for _, s := range a.HealthResult.Signals {
			acc.result.Signals = append(acc.result.Signals, s)
		}
	}

	currentState := HealthState(a.Health)
	finalState := currentState

	if a.KEDAInfo != nil {
		for _, t := range a.KEDAInfo.Triggers {
			if strings.EqualFold(t.Status, "Inactive") || strings.EqualFold(t.Status, "False") {
				acc.AddPenalty("KEDA trigger inactive", w.kedaInactiveTrigger, HealthLimited)
				if currentState != HealthError {
					finalState = HealthLimited
				}
				break
			}
		}
	}

	if a.VPAConflict != nil {
		acc.AddPenalty("VPA conflict detected", w.vpaConflict, HealthLimited)
		if currentState == HealthOK || currentState == HealthStabilized {
			finalState = HealthLimited
		}
	}

	acc.SetState(finalState)
	enrichedResult := acc.Result()
	if enrichedResult.Score < 0 {
		enrichedResult.Score = 0
	}
	a.HealthScore = enrichedResult.Score
	a.Health = string(enrichedResult.State)
	if a.HealthResult != nil {
		a.HealthResult.Score = enrichedResult.Score
		a.HealthResult.State = enrichedResult.State
		a.HealthResult.Signals = enrichedResult.Signals
	}
}
