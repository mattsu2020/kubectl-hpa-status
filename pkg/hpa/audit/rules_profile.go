package audit

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// This file holds the profile-specific audit rules selected by
// profileSpecificRules for the latency, cost, batch, KEDA, and critical
// workload profiles (see RunWithProfile in audit.go).

// profileSpecificRules returns audit rules that apply only when a specific
// workload profile is selected. Each rule applies profile-adjusted thresholds.
func profileSpecificRules(profile Profile) []Rule {
	switch profile {
	case ProfileLatency:
		return []Rule{
			latencyStabilizationRule,
			latencyScaleUpPolicyRule,
		}
	case ProfileCost:
		return []Rule{
			costMinReplicasRule,
			costScaleDownRule,
		}
	case ProfileBatch:
		return []Rule{
			batchToleranceRule,
		}
	case ProfileKEDA:
		return []Rule{
			kedaScaleToZeroRule,
			kedaCooldownRule,
		}
	case ProfileCritical:
		return []Rule{
			criticalMaxHeadroomRule,
			criticalMinReplicasRule,
		}
	default:
		return nil
	}
}

// latencyStabilizationRule warns when the scale-up stabilization window is
// too long for a latency-sensitive workload.
func latencyStabilizationRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleUp == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
	if window == nil || *window <= 60 {
		return nil
	}

	return []Finding{
		{
			ID:          "latency-stabilization",
			Title:       "Scale-up stabilization window too long for latency profile",
			Description: fmt.Sprintf("scaleUp.stabilizationWindowSeconds is %ds. For latency-sensitive workloads, scale-up should respond within 60s. A long stabilization window delays adding capacity under load.", *window),
			Severity:    AuditWarning,
			Category:    "profile-latency",
			Current:     fmt.Sprintf("%ds", *window),
			Recommended: "≤60s for latency-sensitive workloads",
		},
	}
}

// latencyScaleUpPolicyRule warns when no scaleUp policy is configured or when
// the policy period is too long for latency-sensitive workloads.
func latencyScaleUpPolicyRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if util.MissingPolicies(hpa.Spec.Behavior, "scaleUp") {
		return []Finding{
			{
				ID:          "latency-scale-up-policy",
				Title:       "No scaleUp policy for latency-sensitive workload",
				Description: "Latency-sensitive workloads need explicit scaleUp policies to ensure fast, predictable scaling. Without policies, default behavior may be too conservative.",
				Severity:    AuditWarning,
				Category:    "profile-latency",
				Current:     "default scaleUp behavior",
				Recommended: "Add scaleUp policy with PeriodSeconds ≤ 30",
			},
		}
	}

	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleUp == nil {
		return nil
	}

	for _, p := range hpa.Spec.Behavior.ScaleUp.Policies {
		if p.PeriodSeconds > 30 {
			return []Finding{
				{
					ID:          "latency-scale-up-period",
					Title:       "ScaleUp policy period too long for latency profile",
					Description: fmt.Sprintf("A scaleUp policy has PeriodSeconds=%d. For latency-sensitive workloads, scaling should react within 30s windows.", p.PeriodSeconds),
					Severity:    AuditInfo,
					Category:    "profile-latency",
					Current:     fmt.Sprintf("PeriodSeconds=%d", p.PeriodSeconds),
					Recommended: "PeriodSeconds ≤ 30",
				},
			}
		}
	}

	return nil
}

// costMinReplicasRule warns when minReplicas is higher than necessary for a
// cost-optimized workload.
func costMinReplicasRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas <= 2 {
		return nil
	}

	return []Finding{
		{
			ID:          "cost-min-replicas",
			Title:       "minReplicas higher than recommended for cost profile",
			Description: fmt.Sprintf("minReplicas is %d. For cost-optimized workloads, consider lowering to 1-2 to allow aggressive scale-down during low traffic.", minReplicas),
			Severity:    AuditInfo,
			Category:    "profile-cost",
			Current:     fmt.Sprintf("minReplicas=%d", minReplicas),
			Recommended: "1-2 for cost-optimized workloads",
		},
	}
}

// costScaleDownRule warns when scale-down is too slow for cost optimization.
func costScaleDownRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window != nil && *window > 120 {
		return []Finding{
			{
				ID:          "cost-scaledown-window",
				Title:       "Scale-down stabilization window too long for cost profile",
				Description: fmt.Sprintf("scaleDown.stabilizationWindowSeconds is %ds. For cost-optimized workloads, consider reducing to ≤120s to release idle capacity faster.", *window),
				Severity:    AuditInfo,
				Category:    "profile-cost",
				Current:     fmt.Sprintf("%ds", *window),
				Recommended: "≤120s for cost-optimized workloads",
			},
		}
	}

	return nil
}

// batchToleranceRule warns when tolerance is too tight for batch workloads.
func batchToleranceRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	// Default HPA tolerance is 0.1 (10%).
	toleranceValue := 0.1
	if hpa.Spec.Behavior != nil {
		if hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil {
			toleranceValue = hpa.Spec.Behavior.ScaleUp.Tolerance.AsApproximateFloat64()
		} else if hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil {
			toleranceValue = hpa.Spec.Behavior.ScaleDown.Tolerance.AsApproximateFloat64()
		}
	}

	if toleranceValue < 0.3 {
		return []Finding{
			{
				ID:          "batch-tolerance",
				Title:       "Tolerance too tight for batch workload",
				Description: fmt.Sprintf("Tolerance is %.0f%%. Batch workloads should avoid reacting to small metric fluctuations. Consider increasing tolerance to ≥30%% to reduce unnecessary scaling.", toleranceValue*100),
				Severity:    AuditInfo,
				Category:    "profile-batch",
				Current:     fmt.Sprintf("%.0f%%", toleranceValue*100),
				Recommended: "≥30% for batch workloads",
			},
		}
	}

	return nil
}

// kedaScaleToZeroRule recommends scale-to-zero for KEDA-managed workloads
// that still have minReplicas > 0.
func kedaScaleToZeroRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas == 0 {
		return nil
	}

	return []Finding{
		{
			ID:          "keda-scale-to-zero",
			Title:       "KEDA workload not configured for scale-to-zero",
			Description: fmt.Sprintf("minReplicas is %d. KEDA-managed workloads with reliable triggers can benefit from scale-to-zero (minReplicas=0) to save costs during idle periods.", minReplicas),
			Severity:    AuditInfo,
			Category:    "profile-keda",
			Current:     fmt.Sprintf("minReplicas=%d", minReplicas),
			Recommended: "minReplicas=0 with a reliable KEDA trigger",
		},
	}
}

// kedaCooldownRule warns when scale-down stabilization is too long for
// KEDA workloads that should respond quickly to trigger changes.
func kedaCooldownRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window != nil && *window > 300 {
		return []Finding{
			{
				ID:          "keda-cooldown",
				Title:       "Scale-down stabilization too long for KEDA profile",
				Description: fmt.Sprintf("scaleDown.stabilizationWindowSeconds is %ds. KEDA workloads with active triggers should scale down promptly when the trigger signal decreases. Consider reducing to ≤300s.", *window),
				Severity:    AuditInfo,
				Category:    "profile-keda",
				Current:     fmt.Sprintf("%ds", *window),
				Recommended: "≤300s for KEDA-managed workloads",
			},
		}
	}

	return nil
}

// criticalMaxHeadroomRule warns when there is insufficient headroom between
// current and maxReplicas for critical workloads.
func criticalMaxHeadroomRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	current := hpa.Status.CurrentReplicas
	maxRep := hpa.Spec.MaxReplicas

	if current == 0 || maxRep == 0 {
		return nil
	}

	headroom := maxRep - current
	headroomPercent := float64(headroom) / float64(current) * 100

	if headroomPercent < 50 {
		return []Finding{
			{
				ID:          "critical-max-headroom",
				Title:       "Insufficient maxReplicas headroom for critical workload",
				Description: fmt.Sprintf("Current replicas=%d, maxReplicas=%d (%.0f%% headroom). Critical workloads need ≥50%% headroom to absorb sudden traffic spikes. Consider raising maxReplicas.", current, maxRep, headroomPercent),
				Severity:    AuditWarning,
				Category:    "profile-critical",
				Current:     fmt.Sprintf("%d/%d (%.0f%% headroom)", current, maxRep, headroomPercent),
				Recommended: "≥50% headroom for critical workloads",
			},
		}
	}

	return nil
}

// criticalMinReplicasRule warns when minReplicas is below the minimum
// recommended for critical workloads.
func criticalMinReplicasRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas >= 2 {
		return nil
	}

	return []Finding{
		{
			ID:          "critical-min-replicas",
			Title:       "minReplicas too low for critical workload",
			Description: fmt.Sprintf("minReplicas is %d. Critical workloads should maintain at least 2 replicas for high availability during node failures or pod evictions.", minReplicas),
			Severity:    AuditWarning,
			Category:    "profile-critical",
			Current:     fmt.Sprintf("minReplicas=%d", minReplicas),
			Recommended: "≥2 for critical workloads",
		},
	}
}
