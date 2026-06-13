package hpa

import (
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// ExportStructuredDecisionTrace builds a comprehensive, schema-versioned
// decision trace that integrates per-metric analysis, tolerance/stabilization
// effects, and the winning metric determination into a single exportable
// document. Returns nil if hpa is nil.
func ExportStructuredDecisionTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, a Analysis) *StructuredDecisionTrace {
	if hpa == nil {
		return nil
	}

	trace := &StructuredDecisionTrace{
		SchemaVersion:          "v1",
		Namespace:              hpa.Namespace,
		Name:                   hpa.Name,
		CurrentReplicas:        hpa.Status.CurrentReplicas,
		VisibleDesiredReplicas: hpa.Status.DesiredReplicas,
		MinReplicas:            a.Min,
		MaxReplicas:            a.Max,
		Confidence:             ConfidenceMedium,
	}

	// Build per-metric structured entries.
	trace.Metrics = buildStructuredMetricTraces(hpa)

	// Determine winner from MetricDecisionTrace if available, otherwise from metrics.
	trace.WinnerMetric, trace.WinnerConfidence = resolveStructuredWinner(a, trace.Metrics)

	// Estimate raw desired replicas from the largest per-metric desired.
	trace.EstimatedRawDesired = computeEstimatedRawDesired(trace.Metrics, hpa.Status.CurrentReplicas)

	// Build limit clamp description.
	trace.LimitClamp = buildStructuredLimitClamp(trace)

	// Build tolerance trace.
	trace.ToleranceEffect = buildStructuredToleranceTrace(hpa, trace.Metrics)

	// Build stabilization trace.
	trace.StabilizationEffect = buildStructuredStabilizationTrace(hpa, a)

	// Build ordered decision path steps.
	trace.DecisionPath = buildStructuredDecisionPath(trace, hpa)

	// Build summary.
	trace.Summary = buildStructuredSummary(trace)

	return trace
}

// buildStructuredMetricTraces builds per-metric trace entries from HPA current metrics.
func buildStructuredMetricTraces(hpa *autoscalingv2.HorizontalPodAutoscaler) []StructuredMetricTrace {
	var metrics []StructuredMetricTrace

	for _, metric := range hpa.Status.CurrentMetrics {
		name, ratio := metricImpactRatio(hpa, metric)
		if name == "" {
			continue
		}

		current, target := metricDisplayValues(hpa, metric)
		entry := StructuredMetricTrace{
			Name:             name,
			Type:             string(metric.Type),
			Current:          current,
			Target:           target,
			Confidence:       ConfidenceMedium,
			DesiredDirection: "none",
		}

		if ratio != nil {
			entry.Ratio = ratio
			entry.DistanceFromTarget = math.Abs(*ratio - 1.0)

			switch {
			case *ratio > 1.0+defaultTolerance:
				entry.DesiredDirection = "up"
			case *ratio < 1.0-defaultTolerance:
				entry.DesiredDirection = "down"
			default:
				entry.DesiredDirection = "none"
			}

			entry.WithinTolerance = entry.DistanceFromTarget <= defaultTolerance

			if hpa.Status.CurrentReplicas > 0 {
				raw := int32(math.Ceil(float64(hpa.Status.CurrentReplicas) * *ratio))
				entry.EstimatedDesiredReplicas = &raw
				entry.Formula = fmt.Sprintf("ceil(%d * %.3f) = %d", hpa.Status.CurrentReplicas, *ratio, raw)
			}
		} else {
			entry.Confidence = ConfidenceLow
		}

		metrics = append(metrics, entry)
	}

	return metrics
}

// resolveStructuredWinner determines the winning metric from the existing
// MetricDecisionTrace on Analysis, falling back to computing it from the
// structured metric traces.
func resolveStructuredWinner(a Analysis, metrics []StructuredMetricTrace) (string, Confidence) {
	if a.MetricDecisionTrace != nil && a.MetricDecisionTrace.Winner != "" {
		return a.MetricDecisionTrace.Winner, a.MetricDecisionTrace.WinnerConfidence
	}

	if len(metrics) == 0 {
		return "", ""
	}

	var bestName string
	var bestDistance float64

	for _, m := range metrics {
		if m.DistanceFromTarget > bestDistance {
			bestDistance = m.DistanceFromTarget
			bestName = m.Name
		}
	}

	if bestName == "" {
		return "", ""
	}

	return bestName, ConfidenceMedium
}

// computeEstimatedRawDesired returns the largest estimated desired replica count
// from the per-metric traces.
func computeEstimatedRawDesired(metrics []StructuredMetricTrace, _ int32) int32 {
	var maxDesired int32
	for _, m := range metrics {
		if m.EstimatedDesiredReplicas != nil && *m.EstimatedDesiredReplicas > maxDesired {
			maxDesired = *m.EstimatedDesiredReplicas
		}
	}
	return maxDesired
}

// buildStructuredLimitClamp describes whether the desired was clamped by min/max.
func buildStructuredLimitClamp(trace *StructuredDecisionTrace) string {
	if trace.EstimatedRawDesired > trace.MaxReplicas {
		return fmt.Sprintf("maxReplicas=%d caps estimated raw desired replicas %d", trace.MaxReplicas, trace.EstimatedRawDesired)
	}
	if trace.EstimatedRawDesired > 0 && trace.EstimatedRawDesired < trace.MinReplicas {
		return fmt.Sprintf("minReplicas=%d raises estimated raw desired replicas %d", trace.MinReplicas, trace.EstimatedRawDesired)
	}
	if trace.EstimatedRawDesired > 0 {
		return "estimated raw desired replicas are within minReplicas/maxReplicas"
	}
	return "raw desired replicas unavailable"
}

// buildStructuredToleranceTrace builds the tolerance trace section.
func buildStructuredToleranceTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, metrics []StructuredMetricTrace) *ToleranceTrace {
	var suppressed []string
	for _, m := range metrics {
		if m.WithinTolerance {
			suppressed = append(suppressed, m.Name)
		}
	}

	if len(suppressed) == 0 {
		return nil
	}

	trace := &ToleranceTrace{
		DefaultTolerance:   defaultTolerance,
		EffectiveTolerance: defaultTolerance,
		SuppressedMetrics:  suppressed,
	}

	if hpa.Spec.Behavior != nil {
		trace.ConfiguredTolerance = findConfiguredTolerance(hpa.Spec.Behavior)
		if trace.ConfiguredTolerance != nil {
			trace.EffectiveTolerance = *trace.ConfiguredTolerance
		}
	}

	if len(suppressed) == len(metrics) {
		trace.Note = "all metrics within tolerance band, no scaling decision triggered"
	} else {
		trace.Note = fmt.Sprintf("tolerance suppressed scaling for: %s", joinStrings(suppressed, ", "))
	}

	return trace
}

// buildStructuredStabilizationTrace builds the stabilization trace section
// from existing analysis data and HPA signals.
func buildStructuredStabilizationTrace(hpa *autoscalingv2.HorizontalPodAutoscaler, a Analysis) *StabilizationTrace {
	remaining := estimateStabilizationRemaining(hpa)
	if remaining == nil || *remaining <= 0 {
		return nil
	}

	window := scaleDownStabilizationWindow(hpa)
	windowSeconds := int32(0)
	if window != nil {
		windowSeconds = *window
	}

	direction := "scaleDown"
	if a.StabilizationSource != "" {
		direction = a.StabilizationSource
	}

	trace := &StabilizationTrace{
		WindowSeconds:       windowSeconds,
		Direction:           direction,
		RemainingSeconds:    remaining,
		SuppressedDirection: "scaleDown",
		Note:                fmt.Sprintf("stabilization window active, ~%ds remaining", *remaining),
	}

	return trace
}

// buildStructuredDecisionPath creates the ordered list of evaluation steps.
func buildStructuredDecisionPath(trace *StructuredDecisionTrace, _ *autoscalingv2.HorizontalPodAutoscaler) []DecisionStep {
	var steps []DecisionStep
	stepNum := 1

	// Step 1: Read current replicas.
	steps = append(steps, DecisionStep{
		Step:        stepNum,
		Description: "Read current replicas",
		Result:      fmt.Sprintf("currentReplicas=%d", trace.CurrentReplicas),
		Confidence:  ConfidenceHigh,
	})
	stepNum++

	// Step 2+: Per-metric evaluation.
	for _, m := range trace.Metrics {
		result := fmt.Sprintf("ratio=%s", formatRatioSafe(m.Ratio))
		if m.Formula != "" {
			result = fmt.Sprintf("%s formula=%s", result, m.Formula)
		}

		impact := fmt.Sprintf("metric wants %s", m.DesiredDirection)
		if m.WithinTolerance {
			impact = "within tolerance, no scaling"
		}

		steps = append(steps, DecisionStep{
			Step:        stepNum,
			Description: fmt.Sprintf("Evaluate metric %s (%s)", m.Name, m.Type),
			Result:      result,
			Impact:      impact,
			Confidence:  m.Confidence,
		})
		stepNum++
	}

	// Limit clamp step.
	steps = append(steps, DecisionStep{
		Step:        stepNum,
		Description: "Apply minReplicas/maxReplicas limits",
		Result:      trace.LimitClamp,
		Confidence:  ConfidenceHigh,
	})
	stepNum++

	// Tolerance step.
	if trace.ToleranceEffect != nil {
		steps = append(steps, DecisionStep{
			Step:        stepNum,
			Description: "Check tolerance",
			Result:      trace.ToleranceEffect.Note,
			Impact:      fmt.Sprintf("suppressed metrics: %s", joinStrings(trace.ToleranceEffect.SuppressedMetrics, ", ")),
			Confidence:  ConfidenceMedium,
		})
		stepNum++
	}

	// Stabilization step.
	if trace.StabilizationEffect != nil {
		steps = append(steps, DecisionStep{
			Step:        stepNum,
			Description: "Check stabilization window",
			Result:      trace.StabilizationEffect.Note,
			Impact:      fmt.Sprintf("suppressed %s", trace.StabilizationEffect.SuppressedDirection),
			Confidence:  ConfidenceMedium,
		})
		stepNum++
	}

	// Winner determination step.
	if trace.WinnerMetric != "" {
		steps = append(steps, DecisionStep{
			Step:        stepNum,
			Description: "Determine winning metric",
			Result:      fmt.Sprintf("winner=%s", trace.WinnerMetric),
			Impact:      fmt.Sprintf("confidence=%s", string(trace.WinnerConfidence)),
			Confidence:  trace.WinnerConfidence,
		})
		stepNum++
	}

	// Final step.
	steps = append(steps, DecisionStep{
		Step:        stepNum,
		Description: "Produce final desiredReplicas",
		Result:      fmt.Sprintf("desiredReplicas=%d", trace.VisibleDesiredReplicas),
		Confidence:  trace.Confidence,
	})

	return steps
}

// buildStructuredSummary creates a one-line human-readable summary.
func buildStructuredSummary(trace *StructuredDecisionTrace) string {
	if len(trace.Metrics) == 0 {
		return "no metrics available for decision trace"
	}

	if trace.WinnerMetric != "" {
		return fmt.Sprintf("metric %s drove the decision; desiredReplicas=%d (confidence: %s)",
			trace.WinnerMetric, trace.VisibleDesiredReplicas, string(trace.WinnerConfidence))
	}

	return fmt.Sprintf("desiredReplicas=%d from %d metric(s)", trace.VisibleDesiredReplicas, len(trace.Metrics))
}

// formatRatioSafe returns a formatted ratio string, or "unavailable" if nil.
func formatRatioSafe(ratio *float64) string {
	if ratio == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.3f", *ratio)
}

// joinStrings joins non-empty strings with the given separator.
func joinStrings(parts []string, sep string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	result := ""
	for i, s := range nonEmpty {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
