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

	score := healthScoreMax
	health := HealthOK
	var signals []HealthSignal

	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == "ScalingActive" && condition.Status != corev1.ConditionTrue:
			score -= w.scalingInactive
			signals = append(signals, HealthSignal{
				Reason:   "ScalingActive is not True",
				Penalty:  w.scalingInactive,
				Severity: HealthError,
			})
			health = HealthError
		case condition.Type == "AbleToScale" && condition.Status != corev1.ConditionTrue:
			score -= w.unableToScale
			signals = append(signals, HealthSignal{
				Reason:   "AbleToScale is not True",
				Penalty:  w.unableToScale,
				Severity: HealthError,
			})
			health = HealthError
		case condition.Type == "ScalingLimited" && condition.Status == corev1.ConditionTrue:
			score -= w.scalingLimited
			signals = append(signals, HealthSignal{
				Reason:   "ScalingLimited is True",
				Penalty:  w.scalingLimited,
				Severity: HealthLimited,
			})
			if health != HealthError {
				health = HealthLimited
			}
		case condition.Type == "AbleToScale" && condition.Reason == "ScaleDownStabilized":
			score -= w.scaleDownStabilized
			signals = append(signals, HealthSignal{
				Reason:   "ScaleDownStabilized",
				Penalty:  w.scaleDownStabilized,
				Severity: HealthStabilized,
			})
			if health == HealthOK {
				health = HealthStabilized
			}
		}
	}
	if hpa.Status.CurrentReplicas == hpa.Status.DesiredReplicas && hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas {
		hasLimited := hasCondition(hpa.Status.Conditions, "ScalingLimited", corev1.ConditionTrue)
		hasPressure := hasMetricAboveTarget(hpa.Status.CurrentMetrics, hpa)
		if hasLimited || hasPressure {
			score -= w.implicitMaxReplicas
			signals = append(signals, HealthSignal{
				Reason:   "Implicit maxReplicas ceiling (current==desired==max with pressure)",
				Penalty:  w.implicitMaxReplicas,
				Severity: HealthLimited,
			})
			if health == HealthOK {
				health = HealthLimited
			}
		}
	}
	if hpa.Status.DesiredReplicas == minReplicas && hasCondition(hpa.Status.Conditions, "ScalingLimited", corev1.ConditionTrue) {
		score -= w.atMinimumReplicas
		signals = append(signals, HealthSignal{
			Reason:   "At minimum replicas with ScalingLimited",
			Penalty:  w.atMinimumReplicas,
			Severity: health,
		})
	}
	if score < 0 {
		score = 0
	}
	return HealthResult{
		State:   health,
		Score:   score,
		Signals: signals,
	}
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

	if a.KEDAInfo != nil {
		for _, t := range a.KEDAInfo.Triggers {
			if strings.EqualFold(t.Status, "Inactive") || strings.EqualFold(t.Status, "False") {
				a.HealthScore -= w.kedaInactiveTrigger
				if a.Health != string(HealthError) {
					a.Health = string(HealthLimited)
				}
				if a.HealthResult != nil {
					a.HealthResult.Score = a.HealthScore
					a.HealthResult.State = HealthLimited
					a.HealthResult.Signals = append(a.HealthResult.Signals, HealthSignal{
						Reason:   "KEDA trigger inactive",
						Penalty:  w.kedaInactiveTrigger,
						Severity: HealthLimited,
					})
				}
				break
			}
		}
	}

	if a.VPAConflict != nil {
		a.HealthScore -= w.vpaConflict
		if a.Health == string(HealthOK) || a.Health == string(HealthStabilized) {
			a.Health = string(HealthLimited)
		}
		if a.HealthResult != nil {
			a.HealthResult.Score = a.HealthScore
			a.HealthResult.State = HealthLimited
			a.HealthResult.Signals = append(a.HealthResult.Signals, HealthSignal{
				Reason:   "VPA conflict detected",
				Penalty:  w.vpaConflict,
				Severity: HealthLimited,
			})
		}
	}

	if a.HealthScore < 0 {
		a.HealthScore = 0
	}
	if a.HealthResult != nil && a.HealthScore < a.HealthResult.Score {
		a.HealthResult.Score = a.HealthScore
	}
}
