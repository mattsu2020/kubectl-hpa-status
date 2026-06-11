package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func attachHiddenDecisionFactors(a Analysis, hpa *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	if hpa == nil {
		return a
	}
	var factors []HiddenDecisionFactor

	if len(hpa.Spec.Metrics) > len(hpa.Status.CurrentMetrics) {
		factors = append(factors, HiddenDecisionFactor{
			Name:       "Missing metrics",
			Status:     "possible",
			Evidence:   []string{fmt.Sprintf("spec.metrics=%d but status.currentMetrics=%d", len(hpa.Spec.Metrics), len(hpa.Status.CurrentMetrics))},
			Impact:     "HPA may dampen scale-up or scale-down internally when metrics are missing.",
			Confidence: "medium",
		})
	}

	if hpa.Status.DesiredReplicas == hpa.Status.CurrentReplicas {
		for _, metric := range a.Metrics {
			if metric.Ratio == nil {
				continue
			}
			if *metric.Ratio > 0.9 && *metric.Ratio < 1.1 {
				factors = append(factors, HiddenDecisionFactor{
					Name:       "Tolerance",
					Status:     "possible no-op reason",
					Evidence:   []string{fmt.Sprintf("%s ratio %.3f is near the default 10%% tolerance band", metric.Name, *metric.Ratio)},
					Impact:     "The controller may intentionally skip scaling when all metrics remain within tolerance.",
					Confidence: "estimated",
				})
				break
			}
		}
	}

	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		factors = append(factors, HiddenDecisionFactor{
			Name:       "Stabilization window",
			Status:     "active",
			Evidence:   []string{fmt.Sprintf("estimated remaining stabilization time is %ds", *a.StabilizationRemaining)},
			Impact:     "The controller may suppress a scale recommendation until the stabilization window expires.",
			Confidence: "medium",
		})
	}

	if hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas {
		factors = append(factors, HiddenDecisionFactor{
			Name:       "Not-yet-ready pods",
			Status:     "unknown",
			Evidence:   []string{"HPA status does not expose which pods were excluded from CPU calculations."},
			Impact:     "New or not-ready pods can reduce the effective scale-up recommendation for resource metrics.",
			Confidence: "unknown",
		})
	}

	a.HiddenFactors = factors
	return a
}
