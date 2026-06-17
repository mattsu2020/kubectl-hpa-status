package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const adapterVersionEstimation = "estimation-v1"

// EstimateDecisionSignals builds a rich set of DecisionSignal entries from
// available HPA status data. It derives signals from stabilization state,
// metric decision traces, tolerance effects, and condition analysis.
func EstimateDecisionSignals(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	if hpa == nil {
		return nil
	}

	var signals []DecisionSignal

	// Stabilization signal.
	if sig := buildStabilizationDecisionSignal(hpa); sig != nil {
		signals = append(signals, *sig)
	}

	// Metric decision signals from per-metric trace.
	metricSigs := buildMetricDecisionSignals(hpa)
	signals = append(signals, metricSigs...)

	// Tolerance signal.
	if sig := buildToleranceDecisionSignal(hpa); sig != nil {
		signals = append(signals, *sig)
	}

	// Condition-based signals.
	conditionSigs := buildConditionDecisionSignals(hpa)
	signals = append(signals, conditionSigs...)

	// Set adapter version on all signals.
	for i := range signals {
		signals[i].AdapterVersion = adapterVersionEstimation
	}

	return signals
}

// buildStabilizationDecisionSignal creates a signal when the scale-down
// stabilization window is active.
func buildStabilizationDecisionSignal(hpa *autoscalingv2.HorizontalPodAutoscaler) *DecisionSignal {
	remaining := estimateStabilizationRemaining(hpa)
	if remaining == nil || *remaining <= 0 {
		return nil
	}

	window := scaleDownStabilizationWindow(hpa)
	message := "Scale-down is within the stabilization cooldown window"
	if window != nil {
		message = fmt.Sprintf("Scale-down suppressed: %s remaining in %ds stabilization window",
			FormatDuration(*remaining), *window)
	}

	return &DecisionSignal{
		Reason:     "ScaleDownStabilized",
		Message:    message,
		Source:     "StabilizationWindow",
		Confidence: string(ConfidenceMedium),
	}
}

// buildMetricDecisionSignals creates signals from per-metric analysis.
// Each metric that is outside tolerance or has notable impact generates a signal.
func buildMetricDecisionSignals(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	if len(hpa.Status.CurrentMetrics) == 0 {
		return nil
	}

	// Use the metric decision trace infrastructure for analysis.
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	trace := BuildMetricDecisionTrace(hpa, minReplicas)
	if trace == nil {
		return nil
	}

	var signals []DecisionSignal

	// Winner signal.
	if trace.Winner != "" {
		sig := DecisionSignal{
			Reason:     fmt.Sprintf("MetricWinner:%s", trace.Winner),
			Message:    fmt.Sprintf("Metric %s is estimated as the dominant scaling driver (confidence: %s)", trace.Winner, trace.WinnerConfidence),
			MetricName: trace.Winner,
			Source:     "MetricDecisionTrace",
			Confidence: string(trace.WinnerConfidence),
		}
		signals = append(signals, sig)
	}

	// Per-metric signals for metrics outside tolerance.
	for _, entry := range trace.Metrics {
		if entry.WithinTolerance || entry.Ratio == nil {
			continue
		}
		sig := DecisionSignal{
			Reason:     fmt.Sprintf("MetricOutsideTolerance:%s", entry.Name),
			Message:    entry.Note,
			MetricName: entry.Name,
			Source:     "MetricRatio",
			Confidence: string(ConfidenceMedium),
		}
		signals = append(signals, sig)
	}

	// Stabilization effect signal.
	if trace.StabilizationEffect != nil && trace.StabilizationEffect.SuppressedScaleDown {
		sig := DecisionSignal{
			Reason:     "StabilizationEffect",
			Message:    trace.StabilizationEffect.Note,
			Source:     "StabilizationWindow",
			Confidence: string(ConfidenceMedium),
		}
		signals = append(signals, sig)
	}

	// Tolerance effect signal.
	if trace.ToleranceEffect != nil {
		sig := DecisionSignal{
			Reason:     "ToleranceEffect",
			Message:    trace.ToleranceEffect.Note,
			Source:     "Tolerance",
			Confidence: string(ConfidenceHigh),
		}
		signals = append(signals, sig)
	}

	return signals
}

// buildToleranceDecisionSignal creates a signal when tolerance is suppressing
// scaling for all metrics.
func buildToleranceDecisionSignal(hpa *autoscalingv2.HorizontalPodAutoscaler) *DecisionSignal {
	condition := FindCondition(hpa, ConditionAbleToScale)
	if condition == nil {
		return nil
	}
	if condition.Reason != "DesiredWithinTolerance" {
		return nil
	}

	return &DecisionSignal{
		Reason:     "DesiredWithinTolerance",
		Message:    condition.Message,
		Source:     "HPAController",
		Confidence: string(ConfidenceHigh),
	}
}

// buildConditionDecisionSignals creates signals from notable HPA conditions
// that indicate scaling decisions or limitations.
func buildConditionDecisionSignals(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	var signals []DecisionSignal

	for _, condition := range hpa.Status.Conditions {
		switch {
		case condition.Type == autoscalingv2.ScalingActive && condition.Status != "True":
			signals = append(signals, DecisionSignal{
				Reason:     string(condition.Reason),
				Message:    condition.Message,
				Source:     "HPAController",
				Confidence: string(ConfidenceHigh),
			})
		case condition.Type == autoscalingv2.ScalingLimited && condition.Status == "True":
			signals = append(signals, DecisionSignal{
				Reason:     ConditionScalingLimited,
				Message:    condition.Message,
				Source:     "HPAController",
				Confidence: string(ConfidenceHigh),
			})
		}
	}

	return signals
}
