package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DecisionSignalsConfig describes the availability of structured decision data
// from the HPA API server.
type DecisionSignalsConfig struct {
	// APIVersion indicates the detected API capability for decision signals.
	// "v2" means standard autoscaling/v2 (no structured decisions).
	// "v2-alpha+decisions" means KEP-6111 structured output is available.
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	// Available is true when KEP-6111 structured decision output is present
	// on the HPA status object.
	Available bool `json:"available" yaml:"available"`
	// DetectionMethod describes how availability was determined.
	DetectionMethod string `json:"detectionMethod" yaml:"detectionMethod"`
}

// DecisionSignals is the versioned interface for extracting HPA scaling decision
// data. Implementations detect whether KEP-6111 structured output is available
// and convert it into DecisionSignal entries, falling back to estimation when
// structured output is absent.
//
// Expected KEP-6111 API shape (from the draft):
//
//	status.decisions:
//	  - reason: "DesiredWithinTolerance"
//	    message: "the desired replica count is within the tolerance range"
//	    metricName: "cpu"
//	    source: "Resource"
//	    timestamp: "2025-01-15T10:30:00Z"
//
// When available, the adapter converts these fields to DecisionSignal:
//
//	Reason     -> decision.reason
//	Message    -> decision.message
//	MetricName -> decision.metricName
//	Source     -> decision.source
//	Timestamp  -> decision.timestamp
//	Confidence -> "high" (structured output is authoritative)
type DecisionSignals interface {
	// DetectAvailability checks whether KEP-6111 structured output is present.
	DetectAvailability(hpa *autoscalingv2.HorizontalPodAutoscaler) DecisionSignalsConfig
	// FromStructuredOutput converts KEP-6111 structured data when available.
	FromStructuredOutput(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal
	// FromEstimation derives signals from conditions, metrics, and timing.
	FromEstimation(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal
}

// NewDecisionSignalsAdapter returns the appropriate DecisionSignals
// implementation based on the HPA object. Currently returns the estimation
// adapter; will auto-detect KEP-6111 when available.
func NewDecisionSignalsAdapter() DecisionSignals {
	return &estimationAdapter{}
}

// estimationAdapter implements DecisionSignals using best-effort inference
// from HPA status conditions, stabilization timing, metric ratios, and
// tolerance analysis.
type estimationAdapter struct{}

// DetectAvailability reports that structured output is not available
// in current Kubernetes versions.
func (e *estimationAdapter) DetectAvailability(_ *autoscalingv2.HorizontalPodAutoscaler) DecisionSignalsConfig {
	return DecisionSignalsConfig{
		APIVersion:      "v2",
		Available:       false,
		DetectionMethod: "field-absence: no status.decisions in autoscaling/v2",
	}
}

// FromStructuredOutput returns nil for the estimation adapter.
func (e *estimationAdapter) FromStructuredOutput(_ *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	return nil
}

// FromEstimation derives decision signals from HPA status data using
// the comprehensive inference pipeline.
func (e *estimationAdapter) FromEstimation(hpa *autoscalingv2.HorizontalPodAutoscaler) []DecisionSignal {
	return EstimateDecisionSignals(hpa)
}

// detectKEP6111Fields checks whether KEP-6111 structured decision fields are
// present on the HPA status. Currently always returns false as KEP-6111 has
// not landed in any released Kubernetes version.
//
// When KEP-6111 is available, this function should check for:
//   - status.decisions (new field)
//   - Or an annotation like "autoscaling.alpha.kubernetes.io/decision-explainability"
func detectKEP6111Fields(_ *autoscalingv2.HorizontalPodAutoscaler) bool {
	return false
}
