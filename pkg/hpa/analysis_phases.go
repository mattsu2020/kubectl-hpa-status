package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// collectBase populates namespace, name, target, replicas, summary, and
// creation timestamp from the HPA source.
func collectBase(src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) Analysis {
	return Analysis{
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
}

// collectConditions populates the Conditions slice from HPA status conditions,
// sorted by priority.
func collectConditions(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	for _, condition := range prioritizedConditions(src.Status.Conditions) {
		a.Conditions = append(a.Conditions, Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return a
}

// collectMetrics populates the Metrics slice from HPA current metrics.
func collectMetrics(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	for _, metric := range src.Status.CurrentMetrics {
		a.Metrics = append(a.Metrics, FormatMetricStatus(src, metric))
	}
	return a
}

// collectBehavior populates the Behavior slice from HPA spec behavior rules.
func collectBehavior(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	a.Behavior = FormatBehavior(src)
	return a
}

// detectStaleStatus checks for observedGeneration lag and prefixes the summary.
func detectStaleStatus(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	if src.Status.ObservedGeneration != nil && *src.Status.ObservedGeneration < src.Generation {
		a.Summary = "[STALE STATUS] " + a.Summary
		a.StaleStatus = &StaleStatusInfo{
			ObservedGeneration: *src.Status.ObservedGeneration,
			CurrentGeneration:  src.Generation,
			Diff:               src.Generation - *src.Status.ObservedGeneration,
		}
	}
	return a
}

// detectImpactMetric estimates which metric has the largest scaling impact.
func detectImpactMetric(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	guess, ok := MostInfluentialMetric(src)
	if !ok {
		return a
	}
	// When desiredReplicas == maxReplicas, the winner metric cannot be reliably determined
	if src.Status.DesiredReplicas == src.Spec.MaxReplicas {
		guess.Confidence = string(ConfidenceLow)
		guess.Note = "desiredReplicas == maxReplicas so the winner metric cannot be reliably determined"
	} else {
		guess.Confidence = string(ConfidenceMedium)
	}
	a.ImpactMetric = &guess
	return a
}

// detectScaleToZero checks minReplicas==0 and populates cold-start information.
func detectScaleToZero(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) Analysis {
	if minReplicas != 0 {
		return a
	}
	info := &ScaleToZeroInfo{Enabled: true}
	if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas > 0 {
		info.ColdStart = true
		info.Note = "Cold start: scaling from 0 to 1 may experience additional delay; the first metric evaluation must complete before replicas are provisioned."
	} else if src.Status.DesiredReplicas == 0 && src.Status.CurrentReplicas == 0 {
		info.Note = "HPA is at zero replicas (scaled to zero). The next scale-up requires a cold start."
	}
	a.ScaleToZero = info
	return a
}

// detectStabilization estimates remaining seconds in the scale-down
// stabilization window.
func detectStabilization(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler) Analysis {
	if remaining := estimateStabilizationRemaining(src); remaining != nil {
		a.StabilizationRemaining = remaining
	}
	if window := scaleDownStabilizationWindow(src); window != nil {
		a.StabilizationWindowSeconds = window
	}
	return a
}

// attachInterpretation populates interpretation, actions, suggestions, and
// structured outputs when interpretation is enabled.
func attachInterpretation(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, includeInterpretation bool) Analysis {
	if !includeInterpretation {
		return a
	}
	a.Actions = RecommendedActions(src, minReplicas)
	a.Suggestions = BuildSuggestions(src, minReplicas)
	a.Interpretation = Interpret(src, minReplicas)
	a.StructuredInterpretation = buildStructuredInterpretation(src, minReplicas)
	a.StructuredActions = buildStructuredActions(src, minReplicas)
	return a
}

// attachHealth computes and attaches the typed health result.
func attachHealth(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, opts AnalysisOptions) Analysis {
	healthResult := HealthWithWeights(src, minReplicas, opts.HealthWeights)
	a.Health = string(healthResult.State)
	a.HealthScore = healthResult.Score
	if opts.Debug {
		a.HealthResult = &healthResult
	}
	return a
}

// attachDebug adds verbose debug lines when enabled.
func attachDebug(a Analysis, src *autoscalingv2.HorizontalPodAutoscaler, opts AnalysisOptions) Analysis {
	if !opts.Debug {
		return a
	}
	a.Debug = DebugLines(src, a)
	return a
}
