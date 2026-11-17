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
		for _, metric := range a.Metrics.Metrics {
			if metric.Ratio == nil || *metric.Ratio == 1.0 {
				continue
			}
			if within, tol := ratioWithinTolerance(hpa, *metric.Ratio); within {
				factors = append(factors, HiddenDecisionFactor{
					Name:       "Tolerance",
					Status:     "possible no-op reason",
					Evidence:   []string{fmt.Sprintf("%s ratio %.3f is within the %.0f%% tolerance band", metric.Name, *metric.Ratio, tol*100)},
					Impact:     "The controller may intentionally skip scaling when all metrics remain within tolerance.",
					Confidence: string(ConfidenceMedium),
				})
				break
			}
		}
	}

	if a.Conditions.StabilizationActive {
		factors = append(factors, HiddenDecisionFactor{
			Name:       "Stabilization window",
			Status:     "active",
			Evidence:   []string{"HPA reports ScaleDownStabilized; remaining time is unknown"},
			Impact:     "The controller may suppress a scale recommendation until the stabilization window expires.",
			Confidence: string(ConfidenceMedium),
		})
	}

	if hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas {
		factors = append(factors, HiddenDecisionFactor{
			Name:       "Not-yet-ready pods",
			Status:     "unknown",
			Evidence:   []string{"HPA status does not expose which pods were excluded from CPU calculations."},
			Impact:     "New or not-ready pods can reduce the effective scale-up recommendation for resource metrics.",
			Confidence: string(ConfidenceLow),
		})
	}

	a.Lifecycle.HiddenFactors = factors
	return a
}
