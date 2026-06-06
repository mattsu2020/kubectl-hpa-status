package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// stabilizationWindowAuditRule checks whether the scale-down stabilization window
// is explicitly configured. When unset, the controller-manager default (300s)
// applies implicitly, which can surprise operators across Kubernetes upgrades.
func stabilizationWindowAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		return nil
	}

	patch := mustJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
				},
			},
		},
	})

	return []AuditFinding{
		{
			ID:          "stabilization-window",
			Title:       "Stabilization window not explicitly configured",
			Description: "scaleDown.stabilizationWindowSeconds is unset. The controller-manager default (300s) applies implicitly. Explicit configuration prevents surprise behavior changes across Kubernetes upgrades.",
			Severity:    AuditWarning,
			Category:    "stabilization",
			Current:     "unset (default 300s)",
			Recommended: "Set stabilizationWindowSeconds explicitly",
			Patch:       patch,
			Command:     kubectlPatchCommand(hpa, patch, true),
			Risk:        "low",
		},
	}
}

// replicaRangeAuditRule checks for wide replica ranges and unset minReplicas.
func replicaRangeAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []AuditFinding {
	var findings []AuditFinding

	if minReplicas > 0 && hpa.Spec.MaxReplicas/minReplicas > 10 {
		findings = append(findings, AuditFinding{
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
		findings = append(findings, AuditFinding{
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

// behaviorPolicyAuditRule checks for missing explicit scaleUp and scaleDown policies.
func behaviorPolicyAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	var findings []AuditFinding

	if missingPolicies(hpa.Spec.Behavior, "scaleUp") {
		findings = append(findings, AuditFinding{
			ID:          "behavior-policy",
			Title:       "No explicit scaleUp policies configured",
			Description: "Without explicit scaleUp policies, the HPA controller uses default behavior which may not match your workload's scaling needs. Adding policies makes burst response predictable.",
			Severity:    AuditInfo,
			Category:    "behavior",
			Current:     "default scaleUp behavior",
			Recommended: "Add explicit scaleUp policies with bounded rates",
		})
	}

	if missingPolicies(hpa.Spec.Behavior, "scaleDown") {
		findings = append(findings, AuditFinding{
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

// metricCoverageAuditRule checks for single-metric configurations and missing
// resource metrics.
func metricCoverageAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	var findings []AuditFinding

	if len(hpa.Spec.Metrics) == 0 {
		return findings
	}

	if len(hpa.Spec.Metrics) == 1 {
		findings = append(findings, AuditFinding{
			ID:          "metric-coverage",
			Title:       "Single metric configured",
			Description: "Only one metric is configured. A single signal can be noisy or delayed. Consider adding a safety metric (e.g., CPU alongside a queue-depth metric) to improve scaling reliability.",
			Severity:    AuditInfo,
			Category:    "metrics",
			Current:     fmt.Sprintf("1 metric (%s)", metricTypeName(hpa.Spec.Metrics[0])),
			Recommended: "Add a complementary safety metric",
		})
	}

	hasResource := false
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType || spec.Type == autoscalingv2.ContainerResourceMetricSourceType {
			hasResource = true
			break
		}
	}

	if !hasResource {
		findings = append(findings, AuditFinding{
			ID:          "metric-coverage",
			Title:       "No resource metrics configured",
			Description: "All configured metrics are external, custom, pods, or object types. Consider adding CPU or memory as a safety signal; resource metrics are reliably available and can catch scenarios where business metrics are delayed.",
			Severity:    AuditInfo,
			Category:    "metrics",
			Current:     "no resource metrics",
			Recommended: "Add CPU or memory as a safety metric",
		})
	}

	return findings
}

// toleranceAuditRule checks whether the tolerance is explicitly configured.
func toleranceAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	if hpa.Spec.Behavior == nil {
		return []AuditFinding{{
			ID:          "tolerance",
			Title:       "Tolerance uses default value",
			Description: "Tolerance is not explicitly set. The default 0.1 (10%) applies. Workloads with tight scaling requirements may benefit from a narrower tolerance for faster response.",
			Severity:    AuditInfo,
			Category:    "tolerance",
			Current:     "default (0.1 / 10%)",
			Recommended: "Consider setting an explicit tolerance if the default is too loose",
		}}
	}

	hasExplicitTolerance := false
	if hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil {
		hasExplicitTolerance = true
	}
	if hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil {
		hasExplicitTolerance = true
	}

	if hasExplicitTolerance {
		return nil
	}

	return []AuditFinding{{
		ID:          "tolerance",
		Title:       "Tolerance uses default value",
		Description: "Tolerance uses the default 0.1 (10%). Workloads with tight scaling requirements may benefit from a narrower tolerance.",
		Severity:    AuditInfo,
		Category:    "tolerance",
		Current:     "default (0.1 / 10%)",
		Recommended: "Consider setting an explicit tolerance if the default is too loose",
	}}
}

// scaleToZeroAuditRule warns when scale-to-zero is enabled.
func scaleToZeroAuditRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []AuditFinding {
	if minReplicas != 0 {
		return nil
	}

	return []AuditFinding{
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

// resourceRequestAuditRule notes that Resource metrics require corresponding
// resource requests on the pod containers.
func resourceRequestAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	var findings []AuditFinding

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType {
			continue
		}
		name := string(spec.Resource.Name)
		findings = append(findings, AuditFinding{
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

// kedaAuditRule warns when an HPA appears to be KEDA-managed.
func kedaAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	if !looksLikeKEDAManaged(hpa) {
		return nil
	}

	return []AuditFinding{
		{
			ID:          "keda-managed",
			Title:       "HPA appears KEDA-managed",
			Description: "This HPA appears to be managed by KEDA. Direct HPA patches may be overwritten by KEDA reconciliation. Changes should be made on the owning ScaledObject instead.",
			Severity:    AuditInfo,
			Category:    "keda",
			Current:     "KEDA-managed HPA detected",
			Recommended: "Modify the ScaledObject rather than patching the HPA directly",
		},
	}
}

// targetUtilizationAuditRule checks for extreme target utilization values.
func targetUtilizationAuditRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []AuditFinding {
	var findings []AuditFinding

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
			findings = append(findings, AuditFinding{
				ID:          "target-utilization",
				Title:       fmt.Sprintf("High %s target utilization (>90%%)", name),
				Description: fmt.Sprintf("%s target utilization is set to %d%%, which leaves little headroom for traffic bursts. Consider lowering to 70-80%% for production workloads.", name, utilization),
				Severity:    AuditWarning,
				Category:    "target-utilization",
				Current:     fmt.Sprintf("%d%%", utilization),
				Recommended: "70-80% for production workloads",
			})
		} else if utilization < 30 {
			findings = append(findings, AuditFinding{
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

// metricTypeName returns a short display name for a metric spec type.
func metricTypeName(spec autoscalingv2.MetricSpec) string {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if spec.Resource != nil {
			return fmt.Sprintf("Resource/%s", spec.Resource.Name)
		}
		return "Resource"
	case autoscalingv2.ContainerResourceMetricSourceType:
		if spec.ContainerResource != nil {
			return fmt.Sprintf("ContainerResource/%s", spec.ContainerResource.Name)
		}
		return "ContainerResource"
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External != nil {
			return fmt.Sprintf("External/%s", spec.External.Metric.Name)
		}
		return "External"
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods != nil {
			return fmt.Sprintf("Pods/%s", spec.Pods.Metric.Name)
		}
		return "Pods"
	case autoscalingv2.ObjectMetricSourceType:
		if spec.Object != nil {
			return fmt.Sprintf("Object/%s", spec.Object.Metric.Name)
		}
		return "Object"
	default:
		return string(spec.Type)
	}
}
