package hpa

import (
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// Health computes the health state and score using default penalty weights.
func Health(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) (string, int) {
	return HealthWithWeights(hpa, minReplicas, HealthWeights{})
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

// HealthWithWeights computes the health state and score using configurable penalty weights.
func HealthWithWeights(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, weights HealthWeights) (string, int) {
	if hpa == nil {
		return "ERROR", 0
	}
	w := resolveWeights(weights)

	score := healthScoreMax
	health := "OK"
	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == "ScalingActive" && condition.Status != corev1.ConditionTrue:
			score -= w.scalingInactive
			health = "ERROR"
		case condition.Type == "AbleToScale" && condition.Status != corev1.ConditionTrue:
			score -= w.unableToScale
			health = "ERROR"
		case condition.Type == "ScalingLimited" && condition.Status == corev1.ConditionTrue:
			score -= w.scalingLimited
			if health != "ERROR" {
				health = "LIMITED"
			}
		case condition.Type == "AbleToScale" && condition.Reason == "ScaleDownStabilized":
			score -= w.scaleDownStabilized
			if health == "OK" {
				health = "STABILIZED"
			}
		}
	}
	if hpa.Status.CurrentReplicas == hpa.Status.DesiredReplicas && hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas {
		hasLimited := hasCondition(hpa.Status.Conditions, "ScalingLimited", corev1.ConditionTrue)
		hasPressure := hasMetricAboveTarget(hpa.Status.CurrentMetrics, hpa)
		if hasLimited || hasPressure {
			score -= w.implicitMaxReplicas
			if health == "OK" {
				health = "LIMITED"
			}
		}
	}
	if hpa.Status.DesiredReplicas == minReplicas && hasCondition(hpa.Status.Conditions, "ScalingLimited", corev1.ConditionTrue) {
		score -= w.atMinimumReplicas
	}
	if score < 0 {
		score = 0
	}
	return health, score
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
				if a.Health != "ERROR" {
					a.Health = "LIMITED"
				}
				break
			}
		}
	}

	if a.VPAConflict != nil {
		a.HealthScore -= w.vpaConflict
		if a.Health == "OK" || a.Health == "STABILIZED" {
			a.Health = "LIMITED"
		}
	}

	if a.HealthScore < 0 {
		a.HealthScore = 0
	}
}
