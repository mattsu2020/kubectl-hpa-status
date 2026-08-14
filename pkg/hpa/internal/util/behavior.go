// Package util holds small, dependency-free helpers shared across the pkg/hpa
// analysis domains.
package util

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// MissingPolicies reports whether the given behavior lacks scaling policies
// for the specified direction ("scaleUp" or "scaleDown").
func MissingPolicies(behavior *autoscalingv2.HorizontalPodAutoscalerBehavior, direction string) bool {
	if behavior == nil {
		return true
	}
	var rules *autoscalingv2.HPAScalingRules
	switch direction {
	case "scaleUp":
		rules = behavior.ScaleUp
	case "scaleDown":
		rules = behavior.ScaleDown
	default:
		return true
	}
	if rules == nil {
		return true
	}
	return len(rules.Policies) == 0
}
