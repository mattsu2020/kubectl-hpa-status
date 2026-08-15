// Package conditions provides HPA condition lookup helpers and the shared
// stabilization-window math used across the pkg/hpa analysis domains. It is
// the canonical home for condition constants, FindCondition, and the
// stabilization-window estimation; pkg/hpa re-exports these so existing call
// sites keep working, and future sub-packages import this package directly.
package conditions

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/clock"
)

// DefaultScaleDownStabilizationWindowSeconds is the Kubernetes default
// scale-down stabilization window (5 minutes), applied when an HPA does not
// specify one explicitly. It is the single source of truth for the default so
// detection ("is the window at the default?"), suggested patch values, and
// fallback math never drift apart.
const DefaultScaleDownStabilizationWindowSeconds int32 = 300

// HPA condition type constants. Kubernetes defines these as string literals
// in the autoscaling/v2 condition types; we mirror them here so callers
// compare against named constants instead of repeating the magic strings
// ("ScalingActive", "ScalingLimited", "AbleToScale") across the codebase.
//
// Values must stay in sync with autoscalingv2.HorizontalPodAutoscalerCondition.
const (
	// ScalingActive is the HPA condition that reports whether the metrics
	// pipeline can compute a recommendation.
	ScalingActive = "ScalingActive"
	// ScalingLimited is the HPA condition that reports whether scaling is
	// capped by minReplicas or maxReplicas.
	ScalingLimited = "ScalingLimited"
	// AbleToScale is the HPA condition that reports whether the controller
	// can act on scaling decisions.
	AbleToScale = "AbleToScale"
)

// HPA condition reason constants. Kubernetes does not export these as typed
// constants, so the literals are mirrored here once instead of being repeated
// at every call site that inspects condition.Reason.
const (
	// ReasonScaleDownStabilized is the AbleToScale reason set while the
	// scale-down stabilization window suppresses downscaling.
	ReasonScaleDownStabilized = "ScaleDownStabilized"
	// ReasonScaleUpStabilized is the corresponding reason on the scale-up
	// side, set by controllers that apply upscale stabilization.
	ReasonScaleUpStabilized = "ScaleUpStabilized"
)

// Find returns the HPA condition matching the given type, or nil. Returns nil
// safely when hpa is nil.
func Find(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	if hpa == nil {
		return nil
	}
	for i := range hpa.Status.Conditions {
		if string(hpa.Status.Conditions[i].Type) == conditionType {
			return &hpa.Status.Conditions[i]
		}
	}
	return nil
}

// ScaleDownStabilizationWindow returns the HPA scale-down stabilization window
// in seconds, or nil when no behavior is configured.
func ScaleDownStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}
	return hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// EstimateStabilizationRemaining estimates how many seconds remain before the
// scale-down stabilization window expires. Returns nil if the HPA is not in a
// ScaleDownStabilized state or required data is unavailable.
//
// Caveat: Kubernetes downscale stabilization uses the max recommendation
// within the window, not simply LastScaleTime. This estimate is approximate
// and based on LastScaleTime as the best available signal.
func EstimateStabilizationRemaining(hpa *autoscalingv2.HorizontalPodAutoscaler) *int64 {
	condition := Find(hpa, AbleToScale)
	if condition == nil || condition.Reason != ReasonScaleDownStabilized {
		return nil
	}
	window := ScaleDownStabilizationWindow(hpa)
	if window == nil {
		return nil
	}
	if hpa.Status.LastScaleTime == nil {
		return nil
	}
	elapsed := clock.Now().Sub(hpa.Status.LastScaleTime.Time).Seconds()
	remaining := int64(float64(*window) - elapsed)
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}
