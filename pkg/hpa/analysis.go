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
				"[confidence: high] HPA input was nil; no Kubernetes status can be analyzed.",
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
				"[confidence: high] This HPA has no valid scaleTargetRef; it cannot function.",
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
				"[confidence: high] This HPA has spec.maxReplicas set to 0 or negative; it cannot scale.",
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
				fmt.Sprintf("[confidence: high] spec.minReplicas (%d) is greater than spec.maxReplicas (%d); the HPA configuration is contradictory.", minCheck, src.Spec.MaxReplicas),
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
//
//nolint:gocyclo // Sequential analysis pipeline: validate, collect, enrich; each phase is independent.
func AnalyzeWithOptions(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool, opts AnalysisOptions) Analysis {
	if errResult := validateHPA(src); errResult != nil {
		return *errResult
	}

	minReplicas := resolveMinReplicas(src)

	analysis := Analysis{
		Namespace:         src.Namespace,
		Name:              src.Name,
		Target:            fmt.Sprintf("%s/%s", src.Spec.ScaleTargetRef.Kind, src.Spec.ScaleTargetRef.Name),
		Current:           src.Status.CurrentReplicas,
		Desired:           src.Status.DesiredReplicas,
		Min:               minReplicas,
		Max:               src.Spec.MaxReplicas,
		Summary:           SummarizeDirection(src, minReplicas),
		CreationTimestamp: src.CreationTimestamp,
	}

	for _, condition := range prioritizedConditions(src.Status.Conditions) {
		analysis.Conditions = append(analysis.Conditions, Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}

	for _, metric := range src.Status.CurrentMetrics {
		analysis.Metrics = append(analysis.Metrics, FormatMetricStatus(src, metric))
	}

	analysis.Behavior = FormatBehavior(src)

	// Prefix summary with [STALE STATUS] when the controller has not yet observed the latest spec.
	if src.Status.ObservedGeneration != nil && *src.Status.ObservedGeneration < src.Generation {
		analysis.Summary = "[STALE STATUS] " + analysis.Summary
		analysis.StaleStatus = &StaleStatusInfo{
			ObservedGeneration: *src.Status.ObservedGeneration,
			CurrentGeneration:  src.Generation,
			Diff:               src.Generation - *src.Status.ObservedGeneration,
		}
	}

	if guess, ok := MostInfluentialMetric(src); ok {
		// When desiredReplicas == maxReplicas, the winner metric cannot be reliably determined
		if src.Status.DesiredReplicas == src.Spec.MaxReplicas {
			guess.Confidence = "low"
			guess.Note = "desiredReplicas == maxReplicas so the winner metric cannot be reliably determined"
		} else {
			guess.Confidence = "medium"
		}
		analysis.ImpactMetric = &guess
	}

	// Scale-to-zero detection
	if minReplicas == 0 {
		info := &ScaleToZeroInfo{Enabled: true}
		if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas > 0 {
			info.ColdStart = true
			info.Note = "Cold start: scaling from 0 to 1 may experience additional delay; the first metric evaluation must complete before replicas are provisioned."
		} else if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas == 0 {
			info.Note = "HPA is at zero replicas (scaled to zero). The next scale-up requires a cold start."
		}
		analysis.ScaleToZero = info
	}

	// Stabilization remaining time estimation
	if remaining := estimateStabilizationRemaining(src); remaining != nil {
		analysis.StabilizationRemaining = remaining
	}
	if window := scaleDownStabilizationWindow(src); window != nil {
		analysis.StabilizationWindowSeconds = window
	}

	if includeInterpretation {
		analysis.Actions = RecommendedActions(src, minReplicas)
		analysis.Suggestions = BuildSuggestions(src, minReplicas)
		analysis.Interpretation = Interpret(src, minReplicas)
		analysis.Interpretation = append(analysis.Interpretation, KEDADiagnostics(src)...)
		analysis.StructuredInterpretation = buildStructuredInterpretation(src, minReplicas)
		analysis.StructuredActions = buildStructuredActions(src, minReplicas)
	}
	healthResult := HealthWithWeights(src, minReplicas, opts.HealthWeights)
	analysis.Health = string(healthResult.State)
	analysis.HealthScore = healthResult.Score
	if opts.Debug {
		analysis.HealthResult = &healthResult
		analysis.Debug = DebugLines(src, analysis)
	}

	return analysis
}
