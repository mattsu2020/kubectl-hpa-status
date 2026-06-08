package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DecisionAdapter converts upstream HPA decision data into the internal
// DecisionSignal format. When KEP-6111 structured output is available from
// the API server, the adapter converts it. When unavailable, the adapter
// falls back to the current estimation approach.
type DecisionAdapter interface {
	// FromStructuredOutput converts KEP-6111 structured decision data
	// (if available) into DecisionSignal entries. Returns nil when
	// structured output is not available from the API server.
	FromStructuredOutput(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal

	// FromEstimation falls back to the current best-effort estimation
	// approach using status conditions and observed timing.
	FromEstimation(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal
}

// DefaultDecisionAdapter implements DecisionAdapter with the current
// estimation approach. It derives decision signals from HPA status
// conditions, stabilization timing, and metric observations.
type DefaultDecisionAdapter struct{}

// FromStructuredOutput returns nil for the default adapter because
// KEP-6111 structured output is not yet available in current Kubernetes.
func (DefaultDecisionAdapter) FromStructuredOutput(_ *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	return nil
}

// FromEstimation derives decision signals from HPA status data.
// This is the legacy path used when the API server does not support
// KEP-6111 structured decision output.
func (DefaultDecisionAdapter) FromEstimation(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	if hpa == nil {
		return nil
	}
	var signals []DecisionSignal

	// Stabilization signal.
	if remaining := estimateStabilizationRemaining(hpa); remaining != nil && *remaining > 0 {
		window := scaleDownStabilizationWindow(hpa)
		message := "Scale-down is within the stabilization cooldown window"
		if window != nil {
			message = formatStabilizationMessage(int64(*remaining), int64(*window))
		}
		signals = append(signals, DecisionSignal{
			Reason:     "ScaleDownStabilized",
			Message:    message,
			MetricName: "",
			Source:     "StabilizationWindow",
			Confidence: "medium",
		})
	}

	// Scaling conditions signal.
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == autoscalingv2.ScalingActive && condition.Status != "True" {
			signals = append(signals, DecisionSignal{
				Reason:     string(condition.Reason),
				Message:    condition.Message,
				MetricName: "",
				Source:     "HPAController",
				Confidence: "high",
			})
		}
	}

	return signals
}

func formatStabilizationMessage(remaining, window int64) string {
	elapsed := window - remaining
	if elapsed < 0 {
		elapsed = 0
	}
	return FormatStabilizationProgress(&remaining, int32Ptr(int32(window)))
}

func int32Ptr(v int32) *int32 { return &v }

// KEP6111DecisionAdapter will implement DecisionAdapter using structured
// decision output from KEP-6111. This is a placeholder for future
// implementation when the KEP lands in Kubernetes.
//
// Expected API shape (from KEP-6111 draft):
//
//	status.decisions:
//	  - reason: "DesiredWithinTolerance"
//	    message: "..."
//	    metricName: "cpu"
//	    source: "Resource"
//	    timestamp: "..."
//
// The adapter will map these fields to DecisionSignal:
//
//	Reason     -> decision.reason
//	Message    -> decision.message
//	MetricName -> decision.metricName
//	Source     -> decision.source
//	Confidence -> "high" (structured output is authoritative)
type KEP6111DecisionAdapter struct{}

// FromStructuredOutput returns nil until KEP-6111 is available.
func (KEP6111DecisionAdapter) FromStructuredOutput(_ *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	return nil
}

// FromEstimation delegates to DefaultDecisionAdapter for fallback estimation.
func (a KEP6111DecisionAdapter) FromEstimation(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	return DefaultDecisionAdapter{}.FromEstimation(hpa)
}
