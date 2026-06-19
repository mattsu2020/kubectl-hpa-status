// Package vpa analyzes coexistence conflicts between an HPA and a
// VerticalPodAutoscaler targeting the same workload. It is a self-contained
// leaf domain: it depends only on the autoscaling/v2 API types. The cmd/
// layer reaches it through the pkg/hpa re-export facade
// (hpaanalysis.VPAConflictInfo, hpaanalysis.AnalyzeVPA, etc.) so existing
// import paths keep working.
package vpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// RecommendationInfo captures the visible recommendation values for one
// container/resource pair, as extracted from a VPA object.
type RecommendationInfo struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
}

// Info holds the parsed fields of a VerticalPodAutoscaler relevant to HPA
// conflict analysis. This type lives in pkg/hpa/vpa (canonical) and is
// re-exported as hpaanalysis.VPAInfo via a type alias; internal/kube has its
// own VPAInfo for extraction. External consumers build conflict inputs without
// depending on internal/kube by using the pkg/hpa alias.
type Info struct {
	Name                string               `json:"name" yaml:"name"`
	TargetRef           string               `json:"targetRef" yaml:"targetRef"`
	TargetKind          string               `json:"targetKind" yaml:"targetKind"`
	TargetName          string               `json:"targetName" yaml:"targetName"`
	UpdateMode          string               `json:"updateMode" yaml:"updateMode"`
	ControlledResources []string             `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	Recommendations     []RecommendationInfo `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// ConflictInfo holds VPA conflict detection result.
type ConflictInfo struct {
	VPAName             string           `json:"vpaName" yaml:"vpaName"`
	TargetKind          string           `json:"targetKind" yaml:"targetKind"`
	TargetName          string           `json:"targetName" yaml:"targetName"`
	UpdateMode          string           `json:"updateMode,omitempty" yaml:"updateMode,omitempty"`
	ControlledResources []string         `json:"controlledResources,omitempty" yaml:"controlledResources,omitempty"`
	Recommendations     []Recommendation `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	Warning             string           `json:"warning" yaml:"warning"`
}

// Recommendation is the display/API model for one visible VPA recommendation.
type Recommendation struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Lower     string `json:"lower,omitempty" yaml:"lower,omitempty"`
	Upper     string `json:"upper,omitempty" yaml:"upper,omitempty"`
}

// Analyze generates warning lines when VPA conflicts with HPA.
// Returns nil if there is no conflict to report.
func Analyze(hpa *autoscalingv2.HorizontalPodAutoscaler, v *Info) []string {
	if hpa == nil || v == nil {
		return nil
	}

	// Skip if VPA is in "Off" mode — it only recommends, never applies changes.
	if v.UpdateMode == "Off" {
		return nil
	}

	// Only warn when HPA uses CPU or memory resource metrics.
	if !hasHPAResourceMetrics(hpa) {
		return nil
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("[observed] VPA %q targets the same resource %s/%s as this HPA.", v.Name, v.TargetKind, v.TargetName))
	lines = append(lines, "[observed] Both VPA and HPA managing CPU or memory on the same workload can cause conflicting scaling decisions and instability.")
	lines = append(lines, "[observed] Consider setting the VPA updateMode to \"Recommender\" so it only provides recommendations without applying pod overrides, or remove the overlapping resource metric from one of the autoscalers.")

	if v.UpdateMode == "Auto" {
		lines = append(lines, fmt.Sprintf("[observed] VPA %q is in \"Auto\" mode, which will evict and resize pods — this directly conflicts with HPA replica-based scaling.", v.Name))
	}
	for _, rec := range v.Recommendations {
		if !hpaUsesResourceMetric(hpa, rec.Resource) {
			continue
		}
		lines = append(lines, fmt.Sprintf("[estimated] VPA %q recommends %s target=%s for container %q while HPA also scales on %s; compare requests, limits, and HPA target utilization before applying both controllers.", v.Name, rec.Resource, valueOrUnknown(rec.Target), rec.Container, rec.Resource))
	}

	return lines
}

// NewConflictInfo converts extracted VPA data into the public analysis model.
func NewConflictInfo(v *Info) *ConflictInfo {
	if v == nil {
		return nil
	}
	info := &ConflictInfo{
		VPAName:             v.Name,
		TargetKind:          v.TargetKind,
		TargetName:          v.TargetName,
		UpdateMode:          v.UpdateMode,
		ControlledResources: append([]string(nil), v.ControlledResources...),
		Warning:             fmt.Sprintf("VPA %s and HPA both target %s/%s with overlapping resource metrics", v.Name, v.TargetKind, v.TargetName),
	}
	for _, rec := range v.Recommendations {
		info.Recommendations = append(info.Recommendations, Recommendation(rec))
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

// ConflictLevel classifies the severity of a VPA-HPA coexistence conflict.
type ConflictLevel string

const (
	// ConflictNone indicates no active conflict between VPA and HPA.
	ConflictNone ConflictLevel = "NONE"
	// ConflictWarning indicates a potential conflict that warrants monitoring.
	ConflictWarning ConflictLevel = "WARNING"
	// ConflictError indicates both controllers are actively fighting over the same resources.
	ConflictError ConflictLevel = "ERROR"
)

// Advisory provides structured VPA-HPA coexistence analysis with actionable
// recommendations for resolving or mitigating conflicts.
type Advisory struct {
	Level             ConflictLevel `json:"level" yaml:"level"`
	ConflictResources []string      `json:"conflictResources,omitempty" yaml:"conflictResources,omitempty"`
	RecommendedMode   string        `json:"recommendedMode,omitempty" yaml:"recommendedMode,omitempty"`
	Recommendations   []string      `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	SafeCoexistence   bool          `json:"safeCoexistence" yaml:"safeCoexistence"`
	Explanation       string        `json:"explanation,omitempty" yaml:"explanation,omitempty"`
	VPAPatch          string        `json:"vpaPatch,omitempty" yaml:"vpaPatch,omitempty"`
	HPAActions        []string      `json:"hpaActions,omitempty" yaml:"hpaActions,omitempty"`
	VPAActions        []string      `json:"vpaActions,omitempty" yaml:"vpaActions,omitempty"`
}

// AnalyzeAdvisory produces a structured VPA-HPA coexistence advisory.
// Returns nil if either hpa or vpa is nil.
func AnalyzeAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, v *ConflictInfo) *Advisory {
	if hpa == nil || v == nil {
		return nil
	}

	level := determineConflictLevel(hpa, v)
	conflictResources := identifyConflictResources(hpa, v)

	advisory := &Advisory{
		Level:             level,
		ConflictResources: conflictResources,
		SafeCoexistence:   level != ConflictError,
	}

	advisory.Recommendations = generateRecommendations(level, v)
	advisory.RecommendedMode = recommendedModeForLevel(level)
	advisory.Explanation = buildExplanation(level, v, conflictResources)
	advisory.VPAPatch = generateVPAPatch(level)
	advisory.HPAActions = generateHPAActions(level, conflictResources)
	advisory.VPAActions = generateVPAActions(level, v)

	return advisory
}

// determineConflictLevel classifies the VPA-HPA conflict severity.
func determineConflictLevel(hpa *autoscalingv2.HorizontalPodAutoscaler, v *ConflictInfo) ConflictLevel {
	mode := v.UpdateMode

	if mode == "Off" || mode == "Recommender" {
		return ConflictNone
	}

	if !hasHPAResourceMetrics(hpa) {
		return ConflictNone
	}

	if mode == "Initial" {
		return ConflictWarning
	}

	// Auto and any other active mode that evicts/resizes pods.
	return ConflictError
}

// identifyConflictResources returns the subset of VPA-controlled resources
// that the HPA also scales on.
func identifyConflictResources(hpa *autoscalingv2.HorizontalPodAutoscaler, v *ConflictInfo) []string {
	var conflicts []string
	for _, resource := range v.ControlledResources {
		if hpaUsesResourceMetric(hpa, resource) {
			conflicts = append(conflicts, resource)
		}
	}
	return conflicts
}

// generateRecommendations produces actionable recommendation strings for the
// given conflict level.
func generateRecommendations(level ConflictLevel, v *ConflictInfo) []string {
	switch level {
	case ConflictError:
		return []string{
			"Switch VPA updateMode to 'Initial' so it only sets initial requests without evicting pods",
			"Or move HPA to external/custom metrics (RPS, queue depth) to eliminate resource overlap",
			"Review resource requests on the workload to ensure they are appropriate",
		}
	case ConflictWarning:
		return []string{
			"VPA is in 'Initial' mode: safe but monitor for pod restart timing interactions",
			"Consider adding resource requests explicitly to avoid VPA recalculating at each rollout",
		}
	case ConflictNone:
		if v.UpdateMode != "" {
			return []string{
				fmt.Sprintf("VPA is in '%s' mode: no active conflict with HPA", v.UpdateMode),
			}
		}
		return nil
	default:
		return nil
	}
}

// recommendedModeForLevel returns the safest VPA updateMode for the given
// conflict level.
func recommendedModeForLevel(level ConflictLevel) string {
	switch level {
	case ConflictError:
		return "Initial"
	case ConflictWarning:
		return "Off"
	default:
		return ""
	}
}

// generateVPAPatch returns a JSON patch to remediate the conflict, if
// applicable.
func generateVPAPatch(level ConflictLevel) string {
	if level == ConflictError {
		return `{"spec":{"updatePolicy":{"updateMode":"Initial"}}}`
	}
	return ""
}

// buildExplanation produces a human-readable explanation of the conflict.
func buildExplanation(level ConflictLevel, v *ConflictInfo, conflictResources []string) string {
	switch level {
	case ConflictError:
		return fmt.Sprintf(
			"VPA %q is in %q mode and both VPA and HPA are actively managing %v on %s/%s. "+
				"VPA evicts pods to resize resource requests while HPA adjusts replica counts based on utilization. "+
				"This creates a feedback loop where VPA resizing triggers HPA scaling and vice versa, "+
				"leading to unstable workload behavior.",
			v.VPAName, v.UpdateMode, conflictResources,
			v.TargetKind, v.TargetName,
		)
	case ConflictWarning:
		return fmt.Sprintf(
			"VPA %q is in %q mode targeting %s/%s. While VPA only sets initial resource requests "+
				"at pod creation time and does not evict running pods, there is still a potential "+
				"interaction during rollouts when new pods receive VPA-calculated requests that may "+
				"affect HPA utilization calculations.",
			v.VPAName, v.UpdateMode,
			v.TargetKind, v.TargetName,
		)
	case ConflictNone:
		if v.UpdateMode == "Off" {
			return fmt.Sprintf(
				"VPA %q is in %q mode targeting %s/%s: it provides recommendations only "+
					"without applying any changes to pods, so there is no conflict with HPA.",
				v.VPAName, v.UpdateMode,
				v.TargetKind, v.TargetName,
			)
		}
		return fmt.Sprintf(
			"VPA %q is in %q mode targeting %s/%s. The HPA does not use CPU or memory "+
				"resource metrics, so there is no resource overlap between the two controllers.",
			v.VPAName, v.UpdateMode,
			v.TargetKind, v.TargetName,
		)
	default:
		return ""
	}
}

// generateHPAActions returns recommended actions for the HPA side.
func generateHPAActions(level ConflictLevel, conflictResources []string) []string {
	if level == ConflictError {
		actions := []string{
			"Consider replacing resource metrics with external or custom metrics to avoid overlap with VPA",
		}
		if len(conflictResources) > 0 {
			actions = append(actions,
				fmt.Sprintf("Remove %v metric(s) from HPA and let VPA manage those resource requests", conflictResources),
			)
		}
		return actions
	}
	return nil
}

// generateVPAActions returns recommended actions for the VPA side.
func generateVPAActions(level ConflictLevel, v *ConflictInfo) []string {
	switch level {
	case ConflictError:
		return []string{
			fmt.Sprintf("Change VPA %q updateMode from %q to 'Initial' to prevent pod evictions", v.VPAName, v.UpdateMode),
			"Alternatively, set updateMode to 'Off' or 'Recommender' to disable active resource management",
		}
	case ConflictWarning:
		return []string{
			fmt.Sprintf("Consider changing VPA %q updateMode to 'Off' for safest coexistence with HPA", v.VPAName),
		}
	default:
		return nil
	}
}
