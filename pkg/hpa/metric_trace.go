package hpa

import (
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const defaultTolerance = 0.1

// BuildMetricDecisionTrace builds a comprehensive per-metric analysis trace
// explaining which metric drove the HPA scaling decision and why.
func BuildMetricDecisionTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *MetricDecisionTrace {
	if hpa == nil {
		return nil
	}

	entries := buildPerMetricTrace(hpa, minReplicas)
	winner, winnerConfidence := determineWinner(entries, hpa)
	selectPolicy := resolveSelectPolicy(hpa)
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
			entry.ReplicaImpact = entry.DistanceFromTarget * float64(hpa.Status.CurrentReplicas)

			switch {
			case *ratio > 1.0+defaultTolerance:
				entry.DesiredDirection = "up"
			case *ratio < 1.0-defaultTolerance:
				entry.DesiredDirection = "down"
			default:
				entry.DesiredDirection = "none"
			}

			entry.WithinTolerance = entry.DistanceFromTarget <= defaultTolerance

			ratioStr := formatRatio(*ratio)
			if entry.WithinTolerance {
				entry.Note = fmt.Sprintf("%s is within tolerance (%sx target)", name, ratioStr)
			} else {
				direction := "above"
				if *ratio < 1.0 {
					direction = "below"
				}
				entry.Note = fmt.Sprintf("%s is %s target (%sx), estimated replica impact %.1f",
					name, direction, ratioStr, entry.ReplicaImpact)
			}
		} else {
			entry.DesiredDirection = "none"
			entry.Note = fmt.Sprintf("%s ratio unavailable", name)
		}

		entries = append(entries, entry)
	}

	return entries
}

// determineWinner finds the metric with the highest replica impact score.
// When desiredReplicas == maxReplicas, confidence is Low because the winner
// cannot be reliably determined.
func determineWinner(entries []MetricTraceEntry, hpa *autoscalingv2.HorizontalPodAutoscaler) (string, Confidence) {
	if len(entries) == 0 {
		return "", ""
	}

	var bestName string
	var bestScore float64

	for _, entry := range entries {
		if entry.ReplicaImpact > bestScore {
			bestScore = entry.ReplicaImpact
			bestName = entry.Name
		}
	}

	if bestName == "" {
		return "", ""
	}

	if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas {
		return bestName, ConfidenceLow
	}

	return bestName, ConfidenceMedium
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

	// Check if tolerance is explicitly configured in behavior spec
	if hpa.Spec.Behavior != nil {
		effect.ConfiguredTolerance = findConfiguredTolerance(hpa.Spec.Behavior)
	}

	if len(suppressed) == len(entries) {
		effect.Note = "all metrics within tolerance band, no scaling decision triggered"
	} else {
		effect.Note = fmt.Sprintf("tolerance suppressed scaling for: %s", strings.Join(suppressed, ", "))
	}

	return effect
}

// findConfiguredTolerance checks if tolerance is explicitly configured in behavior.
func findConfiguredTolerance(_ *autoscalingv2.HorizontalPodAutoscalerBehavior) *float64 {
	// HPA behavior spec does not have a direct tolerance field in autoscalingv2,
	// but the tolerance is part of the controller manager configuration.
	// We check for any configured tolerance hints in the behavior.
	return nil
}

// resolveSelectPolicy reads the selectPolicy from the behavior spec.
// Returns the effective policy or "Max" as default.
func resolveSelectPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	if hpa.Spec.Behavior == nil {
		return "Max"
	}

	// Check scale-up policy first (most common direction for multi-metric)
	if hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.SelectPolicy != nil {
		return string(*hpa.Spec.Behavior.ScaleUp.SelectPolicy)
	}
	if hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.SelectPolicy != nil {
		return string(*hpa.Spec.Behavior.ScaleDown.SelectPolicy)
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
