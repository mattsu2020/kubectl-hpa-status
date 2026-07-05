package audit

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// stabilizationWindowRule checks whether the scale-down stabilization window
// is explicitly configured. When unset, the controller-manager default (300s)
// applies implicitly, which can surprise operators across Kubernetes upgrades.
func stabilizationWindowRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		return nil
	}

	patch := util.MarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
				},
			},
		},
	})

	return []Finding{
		{
			ID:          "stabilization-window",
			Title:       "Stabilization window not explicitly configured",
			Description: "scaleDown.stabilizationWindowSeconds is unset. The controller-manager default (300s) applies implicitly. Explicit configuration prevents surprise behavior changes across Kubernetes upgrades.",
			Severity:    AuditWarning,
			Category:    "stabilization",
			Current:     "unset (default 300s)",
			Recommended: "Set stabilizationWindowSeconds explicitly",
			Patch:       patch,
			Command:     util.KubectlPatchCommand(hpa, patch),
			Risk:        "low",
		},
	}
}

// behaviorPolicyRule checks for missing explicit scaleUp and scaleDown policies.
func behaviorPolicyRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	if util.MissingPolicies(hpa.Spec.Behavior, "scaleUp") {
		findings = append(findings, Finding{
			ID:          "behavior-policy",
			Title:       "No explicit scaleUp policies configured",
			Description: "Without explicit scaleUp policies, the HPA controller uses default behavior which may not match your workload's scaling needs. Adding policies makes burst response predictable.",
			Severity:    AuditInfo,
			Category:    "behavior",
			Current:     "default scaleUp behavior",
			Recommended: "Add explicit scaleUp policies with bounded rates",
		})
	}

	if util.MissingPolicies(hpa.Spec.Behavior, "scaleDown") {
		findings = append(findings, Finding{
			ID:          "behavior-policy",
			Title:       "No explicit scaleDown policies configured",
			Description: "Without explicit scaleDown policies, the HPA controller uses default behavior which may cause aggressive downscaling. Adding policies keeps downscale predictable.",
			Severity:    AuditInfo,
			Category:    "behavior",
			Current:     "default scaleDown behavior",
			Recommended: "Add explicit scaleDown policies with bounded rates",
		})
	}

	return findings
}

// toleranceRule checks whether the tolerance is explicitly configured.
func toleranceRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil {
		return []Finding{{
			ID:          "tolerance",
			Title:       "Tolerance uses default value",
			Description: "Tolerance is not explicitly set. The default 0.1 (10%) applies. Workloads with tight scaling requirements may benefit from a narrower tolerance for faster response.",
			Severity:    AuditInfo,
			Category:    "tolerance",
			Current:     "default (0.1 / 10%)",
			Recommended: "Consider setting an explicit tolerance if the default is too loose",
		}}
	}

	hasExplicitTolerance := (hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil) ||
		(hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil)

	if hasExplicitTolerance {
		return nil
	}

	return []Finding{{
		ID:          "tolerance",
		Title:       "Tolerance uses default value",
		Description: "Tolerance uses the default 0.1 (10%). Workloads with tight scaling requirements may benefit from a narrower tolerance.",
		Severity:    AuditInfo,
		Category:    "tolerance",
		Current:     "default (0.1 / 10%)",
		Recommended: "Consider setting an explicit tolerance if the default is too loose",
	}}
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
