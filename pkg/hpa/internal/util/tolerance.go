package util

import (
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DefaultTolerance is the tolerance HPA uses when scaleUp/scaleDown.tolerance
// is not explicitly configured on the HPA's behavior.
const DefaultTolerance = 0.1

// DirectionalTolerance returns the effective tolerance for the direction
// implied by ratio. Ratios above one use scaleUp tolerance and ratios below
// one use scaleDown tolerance. The boolean reports whether the value was
// explicitly configured on the HPA.
func DirectionalTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (float64, bool) {
	if hpa == nil || hpa.Spec.Behavior == nil {
		return DefaultTolerance, false
	}

	var rules *autoscalingv2.HPAScalingRules
	if ratio > 1 {
		rules = hpa.Spec.Behavior.ScaleUp
	} else if ratio < 1 {
		rules = hpa.Spec.Behavior.ScaleDown
	}
	if rules == nil || rules.Tolerance == nil {
		return DefaultTolerance, false
	}

	value := rules.Tolerance.AsApproximateFloat64()
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return DefaultTolerance, false
	}
	return value, true
}

// ConfiguredDirectionalTolerances returns the explicitly configured
// scaleUp/scaleDown tolerances, or nil for each direction left at the HPA
// controller default.
func ConfiguredDirectionalTolerances(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown *float64) {
	if hpa == nil || hpa.Spec.Behavior == nil {
		return nil, nil
	}
	if rules := hpa.Spec.Behavior.ScaleUp; rules != nil && rules.Tolerance != nil {
		value := rules.Tolerance.AsApproximateFloat64()
		if !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 {
			scaleUp = &value
		}
	}
	if rules := hpa.Spec.Behavior.ScaleDown; rules != nil && rules.Tolerance != nil {
		value := rules.Tolerance.AsApproximateFloat64()
		if !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 {
			scaleDown = &value
		}
	}
	return scaleUp, scaleDown
}

// EffectiveDirectionalTolerances returns the effective scaleUp/scaleDown
// tolerances, falling back to DefaultTolerance for any direction not
// explicitly configured.
func EffectiveDirectionalTolerances(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown float64) {
	scaleUp, _ = DirectionalTolerance(hpa, 2)
	scaleDown, _ = DirectionalTolerance(hpa, 0)
	return scaleUp, scaleDown
}

// RatioWithinTolerance reports whether ratio is within the effective
// directional tolerance of 1.0 (no scaling needed), along with that
// tolerance value.
func RatioWithinTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (bool, float64) {
	tolerance, _ := DirectionalTolerance(hpa, ratio)
	return math.Abs(ratio-1) <= tolerance, tolerance
}

// EstimatedDesiredForRatio applies the directional tolerance and estimates the
// raw desired replica count for one metric. This mirrors the public part of the
// HPA algorithm; missing-pod and not-yet-ready-pod conservative adjustments are
// intentionally outside this estimate.
func EstimatedDesiredForRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) int32 {
	current := hpa.Status.CurrentReplicas
	within, _ := RatioWithinTolerance(hpa, ratio)
	if within {
		return current
	}
	return int32(math.Ceil(float64(current) * ratio))
}
