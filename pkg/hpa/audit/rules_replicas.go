package audit

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// replicaRangeRule checks for wide replica ranges and unset minReplicas.
func replicaRangeRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	var findings []Finding

	if minReplicas > 0 && hpa.Spec.MaxReplicas/minReplicas > 10 {
		findings = append(findings, Finding{
			ID:          "replica-range",
			Title:       "Wide replica range may indicate instability",
			Description: fmt.Sprintf("maxReplicas/minReplicas ratio is %d (>10x). A wide range can cause large, abrupt scaling events. Consider narrowing the range or adding stepped scaling policies.", hpa.Spec.MaxReplicas/minReplicas),
			Severity:    AuditWarning,
			Category:    "replica-range",
			Current:     fmt.Sprintf("min=%d max=%d (ratio=%d:1)", minReplicas, hpa.Spec.MaxReplicas, hpa.Spec.MaxReplicas/minReplicas),
			Recommended: "Narrow the range to 10x or less, or add explicit scaling policies",
		})
	}

	if hpa.Spec.MinReplicas == nil {
		findings = append(findings, Finding{
			ID:          "replica-range",
			Title:       "minReplicas uses default value",
			Description: "spec.minReplicas is nil, so the Kubernetes default of 1 applies. Explicit configuration makes the intent clear and avoids depending on controller defaults.",
			Severity:    AuditInfo,
			Category:    "replica-range",
			Current:     "unset (default 1)",
			Recommended: "Set minReplicas explicitly",
		})
	}

	return findings
}

// scaleToZeroRule warns when scale-to-zero is enabled.
func scaleToZeroRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas != 0 {
		return nil
	}

	return []Finding{
		{
			ID:          "scale-to-zero",
			Title:       "Scale-to-zero enabled",
			Description: "minReplicas is set to 0, enabling scale-to-zero. Cold start latency may impact availability when traffic resumes. Ensure your workload can tolerate the cold-start delay and that a reliable trigger mechanism is in place.",
			Severity:    AuditWarning,
			Category:    "scale-to-zero",
			Current:     "minReplicas=0",
			Recommended: "Verify cold-start latency is acceptable and triggers are reliable",
		},
	}
}

// resourceRequestRule notes that Resource metrics require corresponding
// resource requests on the pod containers.
func resourceRequestRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType {
			continue
		}
		name := string(spec.Resource.Name)
		findings = append(findings, Finding{
			ID:          "resource-requests",
			Title:       fmt.Sprintf("Verify %s resource requests", name),
			Description: fmt.Sprintf("Resource metric %q is configured. HPA utilization calculations depend on container resource requests being set correctly. Missing or zero requests produce misleading utilization percentages.", name),
			Severity:    AuditInfo,
			Category:    "resources",
			Current:     fmt.Sprintf("resource metric %s configured", name),
			Recommended: fmt.Sprintf("Ensure container %s requests are set on the scale target pods", name),
		})
	}

	return findings
}

// targetUtilizationRule checks for extreme target utilization values.
func targetUtilizationRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType || spec.Resource == nil {
			continue
		}

		target := spec.Resource.Target
		if target.Type != autoscalingv2.UtilizationMetricType || target.AverageUtilization == nil {
			continue
		}

		utilization := *target.AverageUtilization
		name := string(spec.Resource.Name)

		if utilization > 90 {
			findings = append(findings, Finding{
				ID:          "target-utilization",
				Title:       fmt.Sprintf("High %s target utilization (>90%%)", name),
				Description: fmt.Sprintf("%s target utilization is set to %d%%, which leaves little headroom for traffic bursts. Consider lowering to 70-80%% for production workloads.", name, utilization),
				Severity:    AuditWarning,
				Category:    "target-utilization",
				Current:     fmt.Sprintf("%d%%", utilization),
				Recommended: "70-80% for production workloads",
			})
		} else if utilization < 30 {
			findings = append(findings, Finding{
				ID:          "target-utilization",
				Title:       fmt.Sprintf("Low %s target utilization (<30%%)", name),
				Description: fmt.Sprintf("%s target utilization is set to %d%%, which may cause over-provisioning and unnecessary resource costs. Verify this low threshold is intentional.", name, utilization),
				Severity:    AuditInfo,
				Category:    "target-utilization",
				Current:     fmt.Sprintf("%d%%", utilization),
				Recommended: "Typical range is 50-80%; verify this is intentional",
			})
		}
	}

	return findings
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
