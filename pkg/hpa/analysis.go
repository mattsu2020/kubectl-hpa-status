package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DefaultMinReplicas is the default minimum replica count when spec.minReplicas is nil.
const DefaultMinReplicas int32 = 1

// Analyze produces an Analysis for the given HPA using default options.
func Analyze(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool) Analysis {
	return AnalyzeWithOptions(src, includeInterpretation, AnalysisOptions{})
}

// validateHPA checks the HPA for configuration errors and returns an error
// Analysis if validation fails. Returns nil if the HPA is valid.
func validateHPA(src *autoscalingv2.HorizontalPodAutoscaler) *Analysis {
	if src == nil {
		return &Analysis{
			Health:      string(HealthError),
			HealthScore: 0,
			Summary:     "HPA data is unavailable.",
			Interpretation: []string{
				"[observed] HPA input was nil; no Kubernetes status can be analyzed.",
			},
		}
	}

	if src.Spec.ScaleTargetRef.Kind == "" || src.Spec.ScaleTargetRef.Name == "" {
		return &Analysis{
			Namespace:   src.Namespace,
			Name:        src.Name,
			Health:      string(HealthError),
			HealthScore: 0,
			Summary:     "HPA spec.scaleTargetRef is empty or incomplete.",
			Interpretation: []string{
				"[observed] This HPA has no valid scaleTargetRef; it cannot function.",
			},
		}
	}

	if src.Spec.MaxReplicas <= 0 {
		return &Analysis{
			Namespace:   src.Namespace,
			Name:        src.Name,
			Health:      string(HealthError),
			HealthScore: 0,
			Summary:     "HPA spec.maxReplicas must be greater than zero.",
			Interpretation: []string{
				"[observed] This HPA has spec.maxReplicas set to 0 or negative; it cannot scale.",
			},
		}
	}

	minCheck := DefaultMinReplicas
	if src.Spec.MinReplicas != nil {
		minCheck = *src.Spec.MinReplicas
	}
	if minCheck > src.Spec.MaxReplicas {
		return &Analysis{
			Namespace:   src.Namespace,
			Name:        src.Name,
			Health:      string(HealthError),
			HealthScore: 0,
			Summary:     fmt.Sprintf("HPA spec.minReplicas (%d) exceeds spec.maxReplicas (%d).", minCheck, src.Spec.MaxReplicas),
			Interpretation: []string{
				fmt.Sprintf("[observed] spec.minReplicas (%d) is greater than spec.maxReplicas (%d); the HPA configuration is contradictory.", minCheck, src.Spec.MaxReplicas),
			},
		}
	}

	return nil
}

// resolveMinReplicas returns the effective minReplicas value, defaulting to
// DefaultMinReplicas when spec.minReplicas is nil.
func resolveMinReplicas(src *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if src.Spec.MinReplicas != nil {
		return *src.Spec.MinReplicas
	}
	return DefaultMinReplicas
}

// AnalyzeWithOptions produces an Analysis with custom health weights and debug settings.
// The analysis is decomposed into sequential phases for testability and extensibility.
func AnalyzeWithOptions(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool, opts AnalysisOptions) Analysis {
	if errResult := validateHPA(src); errResult != nil {
		return *errResult
	}

	minReplicas := resolveMinReplicas(src)
	a := collectBase(src, minReplicas)
	a = collectConditions(a, src)
	a = collectMetrics(a, src)
	a = collectBehavior(a, src)
	a = detectStaleStatus(a, src)
	a = detectImpactMetric(a, src)
	a = detectMetricDecisionTrace(a, src, minReplicas)
	a = detectScaleToZero(a, src, minReplicas)
	a = detectStabilization(a, src)
	a = attachInterpretation(a, src, minReplicas, includeInterpretation)
	a = attachHealth(a, src, minReplicas, opts)
	a = attachHiddenDecisionFactors(a, src)
	a = correlateStabilizationChurn(a)
	a = attachDecisionSignals(a, src, DefaultDecisionAdapter{})
	a = attachDebug(a, src, opts)
	return a
}
