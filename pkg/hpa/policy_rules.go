package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// PolicyRuleFunc evaluates a single policy rule against an HPA.
// Returns zero or more violations if the HPA does not comply.
type PolicyRuleFunc func(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation

// builtinRules maps rule IDs to their evaluation functions.
var builtinRules = map[string]PolicyRuleFunc{
	"stabilization-window":      stabilizationWindowPolicy,
	"max-replicas-multiplier":   maxReplicasMultiplierPolicy,
	"max-replicas-from-current": maxReplicasFromCurrentPolicy,
	"behavior-policy-required":  behaviorPolicyRequiredPolicy,
	"metric-coverage":           metricCoveragePolicy,
	"target-utilization-range":  targetUtilizationRangePolicy,
	"replica-range":             replicaRangePolicy,
}

// StabilizationWindowPolicy checks that the scaleDown stabilization window
// is within the configured range.
//
// Parameters:
//   - min (int, default 60): minimum allowed stabilization window in seconds
//   - max (int, default 3600): maximum allowed stabilization window in seconds
func stabilizationWindowPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	minSec := params.Int("min", 60)
	maxSec := params.Int("max", 3600)

	var window int32
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		window = *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	} else {
		// Kubernetes default is 300s.
		window = 300
	}

	if int(window) >= minSec && int(window) <= maxSec {
		return nil
	}

	current := fmt.Sprintf("%ds", window)
	required := fmt.Sprintf("%d-%ds", minSec, maxSec)

	return []PolicyViolation{
		{
			RuleID:      "stabilization-window",
			RuleName:    "Stabilization Window Range",
			Severity:    "warning",
			Description: fmt.Sprintf("scaleDown.stabilizationWindowSeconds is %ds, outside allowed range [%ds, %ds]", window, minSec, maxSec),
			Current:     current,
			Required:    required,
		},
	}
}

// MaxReplicasMultiplierPolicy checks that maxReplicas is at least N * minReplicas.
//
// Parameters:
//   - multiplier (int, default 3): minimum maxReplicas/minReplicas ratio
func maxReplicasMultiplierPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	multiplier := params.Int("multiplier", 3)

	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	required := minReplicas * int32(multiplier)
	if hpa.Spec.MaxReplicas >= required {
		return nil
	}

	return []PolicyViolation{
		{
			RuleID:      "max-replicas-multiplier",
			RuleName:    "Max Replicas Multiplier",
			Severity:    "warning",
			Description: fmt.Sprintf("maxReplicas=%d is less than %d×minReplicas (%d×%d=%d)", hpa.Spec.MaxReplicas, multiplier, multiplier, minReplicas, required),
			Current:     fmt.Sprintf("%d", hpa.Spec.MaxReplicas),
			Required:    fmt.Sprintf(">= %d", required),
		},
	}
}

func maxReplicasFromCurrentPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	multiplier := params.Int("maxMultiplierFromCurrent", 5)
	current := hpa.Status.CurrentReplicas
	if current < 1 {
		current = 1
	}
	allowed := current * int32(multiplier)
	if hpa.Spec.MaxReplicas <= allowed {
		return nil
	}
	return []PolicyViolation{
		{
			RuleID:      "max-replicas-from-current",
			RuleName:    "Max Replicas From Current",
			Severity:    "warning",
			Description: fmt.Sprintf("maxReplicas=%d exceeds %d×currentReplicas (%d×%d=%d)", hpa.Spec.MaxReplicas, multiplier, multiplier, current, allowed),
			Current:     fmt.Sprintf("%d", hpa.Spec.MaxReplicas),
			Required:    fmt.Sprintf("<= %d", allowed),
		},
	}
}

// BehaviorPolicyRequiredPolicy checks that explicit scaleUp and/or scaleDown
// policies are configured.
//
// Parameters:
//   - requireScaleUp (bool, default true): require explicit scaleUp policies
//   - requireScaleDown (bool, default true): require explicit scaleDown policies
func behaviorPolicyRequiredPolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	requireUp := params.Bool("requireScaleUp", true)
	requireDown := params.Bool("requireScaleDown", true)

	var violations []PolicyViolation

	if requireUp {
		hasScaleUp := hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && len(hpa.Spec.Behavior.ScaleUp.Policies) > 0
		if !hasScaleUp {
			violations = append(violations, PolicyViolation{
				RuleID:      "behavior-policy-required",
				RuleName:    "Behavior Policy Required",
				Severity:    "info",
				Description: "No explicit scaleUp policies configured; relying on Kubernetes defaults",
				Current:     "none",
				Required:    "explicit scaleUp policies recommended",
			})
		}
	}

	if requireDown {
		hasScaleDown := hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && len(hpa.Spec.Behavior.ScaleDown.Policies) > 0
		if !hasScaleDown {
			violations = append(violations, PolicyViolation{
				RuleID:      "behavior-policy-required",
				RuleName:    "Behavior Policy Required",
				Severity:    "info",
				Description: "No explicit scaleDown policies configured; relying on Kubernetes defaults",
				Current:     "none",
				Required:    "explicit scaleDown policies recommended",
			})
		}
	}

	return violations
}

// MetricCoveragePolicy checks that the HPA has the required metric types.
//
// Parameters:
//   - requireResource (bool, default true): require at least one Resource metric
//   - minMetrics (int, default 1): minimum number of metrics
func metricCoveragePolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	requireResource := params.Bool("requireResource", true)
	minMetrics := params.Int("minMetrics", 1)

	var violations []PolicyViolation

	if len(hpa.Spec.Metrics) < minMetrics {
		violations = append(violations, PolicyViolation{
			RuleID:      "metric-coverage",
			RuleName:    "Metric Coverage",
			Severity:    "warning",
			Description: fmt.Sprintf("HPA has %d metric(s), but at least %d are required", len(hpa.Spec.Metrics), minMetrics),
			Current:     fmt.Sprintf("%d metrics", len(hpa.Spec.Metrics)),
			Required:    fmt.Sprintf(">= %d metrics", minMetrics),
		})
		return violations
	}

	if requireResource {
		hasResource := false
		for _, m := range hpa.Spec.Metrics {
			if m.Type == autoscalingv2.ResourceMetricSourceType {
				hasResource = true
				break
			}
		}
		if !hasResource {
			violations = append(violations, PolicyViolation{
				RuleID:      "metric-coverage",
				RuleName:    "Metric Coverage",
				Severity:    "info",
				Description: "No Resource metric (cpu/memory) configured; consider adding one for reliable scaling",
				Current:     "no resource metrics",
				Required:    "at least one Resource metric recommended",
			})
		}
	}

	return violations
}

// TargetUtilizationRangePolicy checks that resource metric target utilization
// is within the allowed range.
//
// Parameters:
//   - min (int, default 30): minimum allowed target utilization percent
//   - max (int, default 90): maximum allowed target utilization percent
func targetUtilizationRangePolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	minPct := params.Int("min", 30)
	maxPct := params.Int("max", 90)

	var violations []PolicyViolation

	for _, m := range hpa.Spec.Metrics {
		if m.Type != autoscalingv2.ResourceMetricSourceType || m.Resource == nil {
			continue
		}
		if m.Resource.Target.Type != autoscalingv2.UtilizationMetricType || m.Resource.Target.AverageUtilization == nil {
			continue
		}
		util := int(*m.Resource.Target.AverageUtilization)
		name := strings.ToLower(string(m.Resource.Name))

		if util < minPct || util > maxPct {
			violations = append(violations, PolicyViolation{
				RuleID:      "target-utilization-range",
				RuleName:    "Target Utilization Range",
				Severity:    "warning",
				Description: fmt.Sprintf("%s target utilization is %d%%, outside allowed range [%d%%, %d%%]", name, util, minPct, maxPct),
				Current:     fmt.Sprintf("%d%%", util),
				Required:    fmt.Sprintf("%d%%-%d%%", minPct, maxPct),
			})
		}
	}

	return violations
}

// ReplicaRangePolicy checks that the maxReplicas/minReplicas ratio is within bounds.
//
// Parameters:
//   - maxRatio (int, default 10): maximum allowed maxReplicas/minReplicas ratio
func replicaRangePolicy(hpa *autoscalingv2.HorizontalPodAutoscaler, params PolicyParams) []PolicyViolation {
	maxRatio := params.Int("maxRatio", 10)

	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	if minReplicas == 0 {
		return nil
	}

	ratio := int(hpa.Spec.MaxReplicas) / int(minReplicas)
	if ratio <= maxRatio {
		return nil
	}

	return []PolicyViolation{
		{
			RuleID:      "replica-range",
			RuleName:    "Replica Range",
			Severity:    "warning",
			Description: fmt.Sprintf("maxReplicas/minReplicas ratio is %d (max=%d, min=%d), exceeds maximum allowed ratio of %d", ratio, hpa.Spec.MaxReplicas, minReplicas, maxRatio),
			Current:     fmt.Sprintf("ratio=%d", ratio),
			Required:    fmt.Sprintf("<= %d", maxRatio),
		},
	}
}
