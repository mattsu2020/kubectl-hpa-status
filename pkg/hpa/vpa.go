package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// VPAConflictInfo holds VPA conflict detection result.
type VPAConflictInfo struct {
	VPAName    string `json:"vpaName" yaml:"vpaName"`
	TargetKind string `json:"targetKind" yaml:"targetKind"`
	TargetName string `json:"targetName" yaml:"targetName"`
	Warning    string `json:"warning" yaml:"warning"`
}

// AnalyzeVPA generates warning lines when VPA conflicts with HPA.
// Returns nil if there is no conflict to report.
func AnalyzeVPA(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *kube.VPAInfo) []string {
	if hpa == nil || vpa == nil {
		return nil
	}

	// Skip if VPA is in "Off" mode — it only recommends, never applies changes.
	if vpa.UpdateMode == "Off" {
		return nil
	}

	// Only warn when HPA uses CPU or memory resource metrics.
	if !hasHPAResourceMetrics(hpa) {
		return nil
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("[confidence: high] VPA %q targets the same resource %s/%s as this HPA.", vpa.Name, vpa.TargetKind, vpa.TargetName))
	lines = append(lines, "[confidence: high] Both VPA and HPA managing CPU or memory on the same workload can cause conflicting scaling decisions and instability.")
	lines = append(lines, "[confidence: high] Consider setting the VPA updateMode to \"Recommender\" so it only provides recommendations without applying pod overrides, or remove the overlapping resource metric from one of the autoscalers.")

	if vpa.UpdateMode == "Auto" {
		lines = append(lines, fmt.Sprintf("[confidence: high] VPA %q is in \"Auto\" mode, which will evict and resize pods — this directly conflicts with HPA replica-based scaling.", vpa.Name))
	}

	return lines
}

// hasHPAResourceMetrics checks whether the HPA uses CPU or memory resource metrics.
func hasHPAResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource != nil {
			name := string(m.Resource.Name)
			if name == "cpu" || name == "memory" {
				return true
			}
		}
		if m.Type == autoscalingv2.ContainerResourceMetricSourceType && m.ContainerResource != nil {
			name := string(m.ContainerResource.Name)
			if name == "cpu" || name == "memory" {
				return true
			}
		}
	}
	return false
}
