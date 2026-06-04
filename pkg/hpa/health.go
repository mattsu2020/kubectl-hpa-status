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

// HealthWithWeights computes the health state and score using configurable penalty weights.
func HealthWithWeights(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, weights HealthWeights) (string, int) {
	if hpa == nil {
		return "ERROR", 0
	}
	weights = defaultHealthWeights(weights)

	score := healthScoreMax
	health := "OK"
	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == "ScalingActive" && condition.Status != corev1.ConditionTrue:
			score -= weights.ScalingInactive
			health = "ERROR"
		case condition.Type == "AbleToScale" && condition.Status != corev1.ConditionTrue:
			score -= weights.UnableToScale
			health = "ERROR"
		case condition.Type == "ScalingLimited" && condition.Status == corev1.ConditionTrue:
			score -= weights.ScalingLimited
			if health != "ERROR" {
				health = "LIMITED"
			}
		case condition.Type == "AbleToScale" && condition.Reason == "ScaleDownStabilized":
			score -= weights.ScaleDownStabilized
			if health == "OK" {
				health = "STABILIZED"
			}
		}
	}
	if hpa.Status.CurrentReplicas == hpa.Status.DesiredReplicas && hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas {
		score -= weights.ImplicitMaxReplicas
		if health == "OK" {
			health = "LIMITED"
		}
	}
	if hpa.Status.DesiredReplicas == minReplicas {
		score -= weights.AtMinimumReplicas
	}
	if score < 0 {
		score = 0
	}
	return health, score
}

func defaultHealthWeights(weights HealthWeights) HealthWeights {
	if weights.ScalingInactive == 0 {
		weights.ScalingInactive = healthPenaltyScalingInactive
	}
	if weights.UnableToScale == 0 {
		weights.UnableToScale = healthPenaltyUnableToScale
	}
	if weights.ScalingLimited == 0 {
		weights.ScalingLimited = healthPenaltyScalingLimited
	}
	if weights.ImplicitMaxReplicas == 0 {
		weights.ImplicitMaxReplicas = healthPenaltyImplicitMaxReplicas
	}
	if weights.ScaleDownStabilized == 0 {
		weights.ScaleDownStabilized = healthPenaltyScaleDownStabilized
	}
	if weights.AtMinimumReplicas == 0 {
		weights.AtMinimumReplicas = healthPenaltyAtMinimumReplicas
	}
	if weights.KEDAInactiveTrigger == 0 {
		weights.KEDAInactiveTrigger = healthPenaltyKEDAInactiveTrigger
	}
	if weights.VPAConflict == 0 {
		weights.VPAConflict = healthPenaltyVPAConflict
	}
	return weights
}

// ApplyEnrichmentPenalties adjusts the health score and state based on
// KEDA and VPA enrichment data populated after AnalyzeWithOptions.
// This is a post-hoc adjustment that keeps AnalyzeWithOptions clean.
func ApplyEnrichmentPenalties(a *Analysis, weights HealthWeights) {
	if a == nil {
		return
	}
	weights = defaultHealthWeights(weights)

	if a.KEDAInfo != nil {
		for _, t := range a.KEDAInfo.Triggers {
			if strings.EqualFold(t.Status, "Inactive") || strings.EqualFold(t.Status, "False") {
				a.HealthScore -= weights.KEDAInactiveTrigger
				if a.Health != "ERROR" {
					a.Health = "LIMITED"
				}
				break
			}
		}
	}

	if a.VPAConflict != nil {
		a.HealthScore -= weights.VPAConflict
		if a.Health == "OK" || a.Health == "STABILIZED" {
			a.Health = "LIMITED"
		}
	}

	if a.HealthScore < 0 {
		a.HealthScore = 0
	}
}
