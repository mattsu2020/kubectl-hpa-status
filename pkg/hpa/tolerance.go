package hpa

import (
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const defaultTolerance = 0.1

// directionalTolerance returns the effective tolerance for the direction
// implied by ratio. Ratios above one use scaleUp tolerance and ratios below
// one use scaleDown tolerance. The boolean reports whether the value was
// explicitly configured on the HPA.
func directionalTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (float64, bool) {
	if hpa == nil || hpa.Spec.Behavior == nil {
		return defaultTolerance, false
	}

	var rules *autoscalingv2.HPAScalingRules
	if ratio > 1 {
		rules = hpa.Spec.Behavior.ScaleUp
	} else if ratio < 1 {
		rules = hpa.Spec.Behavior.ScaleDown
	}
	if rules == nil || rules.Tolerance == nil {
		return defaultTolerance, false
	}

	value := rules.Tolerance.AsApproximateFloat64()
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return defaultTolerance, false
	}
	return value, true
}

func configuredDirectionalTolerances(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown *float64) {
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

func effectiveDirectionalTolerances(hpa *autoscalingv2.HorizontalPodAutoscaler) (scaleUp, scaleDown float64) {
	scaleUp, _ = directionalTolerance(hpa, 2)
	scaleDown, _ = directionalTolerance(hpa, 0)
	return scaleUp, scaleDown
}

func ratioWithinTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) (bool, float64) {
	tolerance, _ := directionalTolerance(hpa, ratio)
	return math.Abs(ratio-1) <= tolerance, tolerance
}

// estimatedDesiredForRatio applies the directional tolerance and estimates the
// raw desired replica count for one metric. This mirrors the public part of the
// HPA algorithm; missing-pod and not-yet-ready-pod conservative adjustments are
// intentionally outside this estimate.
func estimatedDesiredForRatio(hpa *autoscalingv2.HorizontalPodAutoscaler, ratio float64) int32 {
	current := hpa.Status.CurrentReplicas
	within, _ := ratioWithinTolerance(hpa, ratio)
	if within {
		return current
	}
	return int32(math.Ceil(float64(current) * ratio))
}
