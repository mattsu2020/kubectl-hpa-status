package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// VPARecommendationInfo captures the visible recommendation values for one
// container/resource pair, as extracted from a VPA object.
type VPARecommendationInfo struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
}

// VPAInfo holds the parsed fields of a VerticalPodAutoscaler relevant to HPA
// conflict analysis. This type lives in pkg/hpa so external consumers can
// build conflict inputs without depending on internal/kube; internal/kube
// re-exports it as VPAInfo for backwards compatibility.
type VPAInfo struct {
	Name                string                  `json:"name" yaml:"name"`
	TargetRef           string                  `json:"targetRef" yaml:"targetRef"`
	TargetKind          string                  `json:"targetKind" yaml:"targetKind"`
	TargetName          string                  `json:"targetName" yaml:"targetName"`
	UpdateMode          string                  `json:"updateMode" yaml:"updateMode"`
	ControlledResources []string                `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	Recommendations     []VPARecommendationInfo `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// VPAConflictInfo holds VPA conflict detection result.
type VPAConflictInfo struct {
	VPAName             string              `json:"vpaName" yaml:"vpaName"`
	TargetKind          string              `json:"targetKind" yaml:"targetKind"`
	TargetName          string              `json:"targetName" yaml:"targetName"`
	UpdateMode          string              `json:"updateMode,omitempty" yaml:"updateMode,omitempty"`
	ControlledResources []string            `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	Recommendations     []VPARecommendation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	Warning             string              `json:"warning" yaml:"warning"`
}

// VPARecommendation is the display/API model for one visible VPA recommendation.
type VPARecommendation struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
}

// AnalyzeVPA generates warning lines when VPA conflicts with HPA.
// Returns nil if there is no conflict to report.
func AnalyzeVPA(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *VPAInfo) []string {
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

	lines = append(lines, fmt.Sprintf("[observed] VPA %q targets the same resource %s/%s as this HPA.", vpa.Name, vpa.TargetKind, vpa.TargetName))
	lines = append(lines, "[observed] Both VPA and HPA managing CPU or memory on the same workload can cause conflicting scaling decisions and instability.")
	lines = append(lines, "[observed] Consider setting the VPA updateMode to \"Recommender\" so it only provides recommendations without applying pod overrides, or remove the overlapping resource metric from one of the autoscalers.")

	if vpa.UpdateMode == "Auto" {
		lines = append(lines, fmt.Sprintf("[observed] VPA %q is in \"Auto\" mode, which will evict and resize pods — this directly conflicts with HPA replica-based scaling.", vpa.Name))
	}
	for _, rec := range vpa.Recommendations {
		if !hpaUsesResourceMetric(hpa, rec.Resource) {
			continue
		}
		lines = append(lines, fmt.Sprintf("[estimated] VPA %q recommends %s target=%s for container %q while HPA also scales on %s; compare requests, limits, and HPA target utilization before applying both controllers.", vpa.Name, rec.Resource, valueOrUnknown(rec.Target), rec.Container, rec.Resource))
	}

	return lines
}

// NewVPAConflictInfo converts extracted VPA data into the public analysis model.
func NewVPAConflictInfo(vpa *VPAInfo) *VPAConflictInfo {
	if vpa == nil {
		return nil
	}
	info := &VPAConflictInfo{
		VPAName:             vpa.Name,
		TargetKind:          vpa.TargetKind,
		TargetName:          vpa.TargetName,
		UpdateMode:          vpa.UpdateMode,
		ControlledResources: append([]string(nil), vpa.ControlledResources...),
		Warning:             fmt.Sprintf("VPA %s and HPA both target %s/%s with overlapping resource metrics", vpa.Name, vpa.TargetKind, vpa.TargetName),
	}
	for _, rec := range vpa.Recommendations {
		info.Recommendations = append(info.Recommendations, VPARecommendation(rec))
	}
	return info
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

func hpaUsesResourceMetric(hpa *autoscalingv2.HorizontalPodAutoscaler, resource string) bool {
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource != nil && string(m.Resource.Name) == resource {
			return true
		}
		if m.Type == autoscalingv2.ContainerResourceMetricSourceType && m.ContainerResource != nil && string(m.ContainerResource.Name) == resource {
			return true
		}
	}
	return false
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
}
