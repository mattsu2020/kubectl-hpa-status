package hpa

import (
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// BuildMetricDecisionTrace builds a comprehensive per-metric analysis trace
// explaining which metric drove the HPA scaling decision and why.
func BuildMetricDecisionTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *MetricDecisionTrace {
	if hpa == nil {
		return nil
	}

	entries := buildPerMetricTrace(hpa, minReplicas)
	winner, winnerConfidence := determineWinner(entries, hpa)
	selectPolicy := resolveSelectPolicy(hpa, entries, winner)
	stabEffect := buildStabilizationEffect(hpa)
	tolEffect := buildToleranceEffect(hpa, entries)
	summary := buildTraceSummary(entries, winner, winnerConfidence)

	return &MetricDecisionTrace{
		Metrics:             entries,
		Winner:              winner,
		WinnerConfidence:    winnerConfidence,
		SelectPolicy:        selectPolicy,
		StabilizationEffect: stabEffect,
		ToleranceEffect:     tolEffect,
		Summary:             summary,
	}
}

// buildPerMetricTrace iterates current metrics and builds a MetricTraceEntry for each.
func buildPerMetricTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []MetricTraceEntry {
	var entries []MetricTraceEntry

	for _, metric := range hpa.Status.CurrentMetrics {
		name, ratio := metricImpactRatio(hpa, metric)
		if name == "" {
			continue
		}

		entry := MetricTraceEntry{
			Name: name,
			Type: string(metric.Type),
		}

		if ratio != nil {
			entry.Ratio = ratio
			entry.DistanceFromTarget = math.Abs(*ratio - 1.0)
			withinTolerance, tolerance := ratioWithinTolerance(hpa, *ratio)
			entry.WithinTolerance = withinTolerance
			entry.EffectiveTolerance = tolerance
			estimatedDesired := estimatedDesiredForRatio(hpa, *ratio)
			entry.EstimatedDesiredReplicas = &estimatedDesired
			entry.ReplicaImpact = float64(estimatedDesired - hpa.Status.CurrentReplicas)

			switch {
			case *ratio > 1.0+tolerance:
				entry.DesiredDirection = "up"
			case *ratio < 1.0-tolerance:
				entry.DesiredDirection = "down"
			default:
				entry.DesiredDirection = "none"
			}

			ratioStr := formatRatio(*ratio)
			if entry.WithinTolerance {
				entry.Note = fmt.Sprintf("%s is within %s tolerance %.3f (%sx target)",
					name, toleranceDirection(*ratio), tolerance, ratioStr)
			} else {
				direction := "above"
				if *ratio < 1.0 {
					direction = "below"
				}
				entry.Note = fmt.Sprintf("%s is %s target (%sx), estimated desired replicas %d (impact %+.0f)",
					name, direction, ratioStr, estimatedDesired, entry.ReplicaImpact)
			}
		} else {
			entry.DesiredDirection = "none"
			entry.Note = fmt.Sprintf("%s ratio unavailable", name)
		}

		entries = append(entries, entry)
	}

	return entries
}

// determineWinner finds the metric with the highest estimated desired replica
// count, matching the HPA controller's multi-metric selection rule.
// When desiredReplicas == maxReplicas, confidence is Low because the winner
// cannot be reliably determined.
func determineWinner(entries []MetricTraceEntry, hpa *autoscalingv2.HorizontalPodAutoscaler) (string, Confidence) {
	if len(entries) == 0 {
		return "", ""
	}

	var bestName string
	var bestDesired int32
	found := false
	bestCount := 0

	for _, entry := range entries {
		if entry.EstimatedDesiredReplicas != nil && (!found || *entry.EstimatedDesiredReplicas > bestDesired) {
			bestDesired = *entry.EstimatedDesiredReplicas
			bestName = entry.Name
			found = true
			bestCount = 1
		} else if entry.EstimatedDesiredReplicas != nil && *entry.EstimatedDesiredReplicas == bestDesired {
			bestCount++
		}
	}

	if !found {
		return "", ""
	}

	if bestCount > 1 || winnerHiddenByControllerState(hpa) {
		return bestName, ConfidenceLow
	}

	return bestName, ConfidenceMedium
}

func winnerHiddenByControllerState(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if hpa.Status.DesiredReplicas >= hpa.Spec.MaxReplicas {
		return true
	}
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	if hpa.Status.DesiredReplicas <= minReplicas {
		minClampHidesWinner := true
		for _, metric := range hpa.Status.CurrentMetrics {
			_, ratio := metricImpactRatio(hpa, metric)
			if ratio != nil && estimatedDesiredForRatio(hpa, *ratio) > minReplicas {
				minClampHidesWinner = false
				break
			}
		}
		if minClampHidesWinner {
			return true
		}
	}
	if len(hpa.Status.CurrentMetrics) < len(hpa.Spec.Metrics) {
		return true
	}
	condition := FindCondition(hpa, ConditionAbleToScale)
	return condition != nil && condition.Reason == "ScaleDownStabilized"
}

func toleranceDirection(ratio float64) string {
	if ratio > 1 {
		return "scaleUp"
	}
	if ratio < 1 {
		return "scaleDown"
	}
	return "effective"
}

// buildStabilizationEffect checks whether scale-down stabilization is active
// and builds the effect description.
func buildStabilizationEffect(hpa *autoscalingv2.HorizontalPodAutoscaler) *StabilizationEffect {
	condition := FindCondition(hpa, ConditionAbleToScale)
	if condition == nil || condition.Reason != "ScaleDownStabilized" {
		return nil
	}

	window := scaleDownStabilizationWindow(hpa)
	if window == nil {
		return nil
	}

	effect := &StabilizationEffect{
		WindowSeconds:       *window,
		SuppressedScaleDown: true,
	}

	remaining := estimateStabilizationRemaining(hpa)
	if remaining != nil {
		effect.RemainingSeconds = remaining
		effect.Note = fmt.Sprintf("scale-down suppressed, ~%ds remaining in %ds stabilization window", *remaining, *window)
	} else {
		effect.Note = fmt.Sprintf("scale-down suppressed by %ds stabilization window", *window)
	}

	return effect
}

// buildToleranceEffect checks which metrics are within tolerance and builds
// the tolerance effect description.
func buildToleranceEffect(hpa *autoscalingv2.HorizontalPodAutoscaler, entries []MetricTraceEntry) *ToleranceEffect {
	var suppressed []string

	for _, entry := range entries {
		if entry.WithinTolerance {
			suppressed = append(suppressed, entry.Name)
		}
	}

	if len(suppressed) == 0 {
		return nil
	}

	effect := &ToleranceEffect{
		DefaultTolerance:  defaultTolerance,
		SuppressedMetrics: suppressed,
	}
	effect.ScaleUpTolerance, effect.ScaleDownTolerance = effectiveDirectionalTolerances(hpa)
	effect.ConfiguredScaleUpTolerance, effect.ConfiguredScaleDownTolerance = configuredDirectionalTolerances(hpa)
	if effect.ConfiguredScaleUpTolerance != nil && effect.ConfiguredScaleDownTolerance != nil &&
		*effect.ConfiguredScaleUpTolerance == *effect.ConfiguredScaleDownTolerance {
		effect.ConfiguredTolerance = effect.ConfiguredScaleUpTolerance
	}

	if len(suppressed) == len(entries) {
		effect.Note = fmt.Sprintf("all metrics within directional tolerance bands (scaleUp=%.3f, scaleDown=%.3f), no scaling decision triggered",
			effect.ScaleUpTolerance, effect.ScaleDownTolerance)
	} else {
		effect.Note = fmt.Sprintf("tolerance suppressed scaling for: %s", strings.Join(suppressed, ", "))
	}

	return effect
}

// resolveSelectPolicy reads the selectPolicy from the behavior spec.
// Returns the effective policy or "Max" as default.
func resolveSelectPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, entries []MetricTraceEntry, winner string) string {
	if hpa.Spec.Behavior == nil {
		return "Max"
	}

	direction := ""
	for _, entry := range entries {
		if entry.Name == winner {
			direction = entry.DesiredDirection
			break
		}
	}
	if direction == "down" {
		if rules := hpa.Spec.Behavior.ScaleDown; rules != nil && rules.SelectPolicy != nil {
			return string(*rules.SelectPolicy)
		}
	} else if rules := hpa.Spec.Behavior.ScaleUp; rules != nil && rules.SelectPolicy != nil {
		return string(*rules.SelectPolicy)
	}

	return "Max"
}

// buildTraceSummary creates a one-line human-readable summary of the decision trace.
func buildTraceSummary(entries []MetricTraceEntry, winner string, winnerConfidence Confidence) string {
	if len(entries) == 0 {
		return "no metrics available for decision trace"
	}

	var parts []string

	for _, entry := range entries {
		if entry.Ratio == nil {
			parts = append(parts, fmt.Sprintf("%s has no ratio data", entry.Name))
			continue
		}

		ratioStr := formatRatio(*entry.Ratio)
		switch {
		case entry.Name == winner:
			parts = append(parts, fmt.Sprintf("%s is dominant (%sx target)", entry.Name, ratioStr))
		case entry.WithinTolerance:
			parts = append(parts, fmt.Sprintf("%s is within tolerance (%sx)", entry.Name, ratioStr))
		default:
			parts = append(parts, fmt.Sprintf("%s wants %s (%sx)", entry.Name, entry.DesiredDirection, ratioStr))
		}
	}

	summary := strings.Join(parts, "; ")

	if winner != "" && winnerConfidence == ConfidenceLow {
		summary += " (winner confidence is low: desiredReplicas == maxReplicas)"
	}

	return summary
}

// formatRatio formats a ratio value for display.
func formatRatio(r float64) string {
	return fmt.Sprintf("%.2f", r)
}
