package audit

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// metricCoverageRule checks for single-metric configurations and missing
// resource metrics.
func metricCoverageRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	if len(hpa.Spec.Metrics) == 0 {
		return findings
	}

	if len(hpa.Spec.Metrics) == 1 {
		findings = append(findings, Finding{
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
		findings = append(findings, Finding{
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

// kedaRule warns when an HPA appears to be KEDA-managed.
func kedaRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if !util.LooksLikeKEDAManaged(hpa) {
		return nil
	}

	return []Finding{
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
