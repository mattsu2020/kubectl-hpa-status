package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// VPAConflictLevel classifies the severity of a VPA-HPA coexistence conflict.
type VPAConflictLevel string

const (
	// VPAConflictNone indicates no active conflict between VPA and HPA.
	VPAConflictNone VPAConflictLevel = "NONE"
	// VPAConflictWarning indicates a potential conflict that warrants monitoring.
	VPAConflictWarning VPAConflictLevel = "WARNING"
	// VPAConflictError indicates both controllers are actively fighting over the same resources.
	VPAConflictError VPAConflictLevel = "ERROR"
)

// VPAAdvisory provides structured VPA-HPA coexistence analysis with actionable
// recommendations for resolving or mitigating conflicts.
type VPAAdvisory struct {
	Level             VPAConflictLevel `json:"level" yaml:"level"`
	ConflictResources []string         `json:"conflictResources,omitempty" yaml:"conflictResources,omitempty"`
	RecommendedMode   string           `json:"recommendedMode,omitempty" yaml:"recommendedMode,omitempty"`
	Recommendations   []string         `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	SafeCoexistence   bool             `json:"safeCoexistence" yaml:"safeCoexistence"`
	Explanation       string           `json:"explanation,omitempty" yaml:"explanation,omitempty"`
	VPAPatch          string           `json:"vpaPatch,omitempty" yaml:"vpaPatch,omitempty"`
	HPAActions        []string         `json:"hpaActions,omitempty" yaml:"hpaActions,omitempty"`
	VPAActions        []string         `json:"vpaActions,omitempty" yaml:"vpaActions,omitempty"`
}

// AnalyzeVPAAdvisory produces a structured VPA-HPA coexistence advisory.
// Returns nil if either hpa or vpa is nil.
func AnalyzeVPAAdvisory(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *VPAConflictInfo) *VPAAdvisory {
	if hpa == nil || vpa == nil {
		return nil
	}

	level := determineConflictLevel(hpa, vpa)
	conflictResources := identifyConflictResources(hpa, vpa)

	advisory := &VPAAdvisory{
		Level:             level,
		ConflictResources: conflictResources,
		SafeCoexistence:   level != VPAConflictError,
	}

	advisory.Recommendations = generateRecommendations(level, vpa)
	advisory.RecommendedMode = recommendedModeForLevel(level)
	advisory.Explanation = buildExplanation(level, vpa, conflictResources)
	advisory.VPAPatch = generateVPAPatch(level)
	advisory.HPAActions = generateHPAActions(level, conflictResources)
	advisory.VPAActions = generateVPAActions(level, vpa)

	return advisory
}

// determineConflictLevel classifies the VPA-HPA conflict severity.
func determineConflictLevel(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *VPAConflictInfo) VPAConflictLevel {
	mode := vpa.UpdateMode

	if mode == "Off" || mode == "Recommender" {
		return VPAConflictNone
	}

	if !hasHPAResourceMetrics(hpa) {
		return VPAConflictNone
	}

	if mode == "Initial" {
		return VPAConflictWarning
	}

	// Auto and any other active mode that evicts/resizes pods.
	return VPAConflictError
}

// identifyConflictResources returns the subset of VPA-controlled resources
// that the HPA also scales on.
func identifyConflictResources(hpa *autoscalingv2.HorizontalPodAutoscaler, vpa *VPAConflictInfo) []string {
	var conflicts []string
	for _, resource := range vpa.ControlledResources {
		if hpaUsesResourceMetric(hpa, resource) {
			conflicts = append(conflicts, resource)
		}
	}
	return conflicts
}

// generateRecommendations produces actionable recommendation strings for the
// given conflict level.
func generateRecommendations(level VPAConflictLevel, vpa *VPAConflictInfo) []string {
	switch level {
	case VPAConflictError:
		return []string{
			"Switch VPA updateMode to 'Initial' so it only sets initial requests without evicting pods",
			"Or move HPA to external/custom metrics (RPS, queue depth) to eliminate resource overlap",
			"Review resource requests on the workload to ensure they are appropriate",
		}
	case VPAConflictWarning:
		return []string{
			fmt.Sprintf("VPA is in 'Initial' mode: safe but monitor for pod restart timing interactions"),
			"Consider adding resource requests explicitly to avoid VPA recalculating at each rollout",
		}
	case VPAConflictNone:
		if vpa.UpdateMode != "" {
			return []string{
				fmt.Sprintf("VPA is in '%s' mode: no active conflict with HPA", vpa.UpdateMode),
			}
		}
		return nil
	default:
		return nil
	}
}

// recommendedModeForLevel returns the safest VPA updateMode for the given
// conflict level.
func recommendedModeForLevel(level VPAConflictLevel) string {
	switch level {
	case VPAConflictError:
		return "Initial"
	case VPAConflictWarning:
		return "Off"
	default:
		return ""
	}
}

// generateVPAPatch returns a JSON patch to remediate the conflict, if
// applicable.
func generateVPAPatch(level VPAConflictLevel) string {
	if level == VPAConflictError {
		return `{"spec":{"updatePolicy":{"updateMode":"Initial"}}}`
	}
	return ""
}

// buildExplanation produces a human-readable explanation of the conflict.
func buildExplanation(level VPAConflictLevel, vpa *VPAConflictInfo, conflictResources []string) string {
	switch level {
	case VPAConflictError:
		return fmt.Sprintf(
			"VPA %q is in %q mode and both VPA and HPA are actively managing %v on %s/%s. "+
				"VPA evicts pods to resize resource requests while HPA adjusts replica counts based on utilization. "+
				"This creates a feedback loop where VPA resizing triggers HPA scaling and vice versa, "+
				"leading to unstable workload behavior.",
			vpa.VPAName, vpa.UpdateMode, conflictResources,
			vpa.TargetKind, vpa.TargetName,
		)
	case VPAConflictWarning:
		return fmt.Sprintf(
			"VPA %q is in %q mode targeting %s/%s. While VPA only sets initial resource requests "+
				"at pod creation time and does not evict running pods, there is still a potential "+
				"interaction during rollouts when new pods receive VPA-calculated requests that may "+
				"affect HPA utilization calculations.",
			vpa.VPAName, vpa.UpdateMode,
			vpa.TargetKind, vpa.TargetName,
		)
	case VPAConflictNone:
		if vpa.UpdateMode == "Off" {
			return fmt.Sprintf(
				"VPA %q is in %q mode targeting %s/%s: it provides recommendations only "+
					"without applying any changes to pods, so there is no conflict with HPA.",
				vpa.VPAName, vpa.UpdateMode,
				vpa.TargetKind, vpa.TargetName,
			)
		}
		return fmt.Sprintf(
			"VPA %q is in %q mode targeting %s/%s. The HPA does not use CPU or memory "+
				"resource metrics, so there is no resource overlap between the two controllers.",
			vpa.VPAName, vpa.UpdateMode,
			vpa.TargetKind, vpa.TargetName,
		)
	default:
		return ""
	}
}

// generateHPAActions returns recommended actions for the HPA side.
func generateHPAActions(level VPAConflictLevel, conflictResources []string) []string {
	if level == VPAConflictError {
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
func generateVPAActions(level VPAConflictLevel, vpa *VPAConflictInfo) []string {
	switch level {
	case VPAConflictError:
		return []string{
			fmt.Sprintf("Change VPA %q updateMode from %q to 'Initial' to prevent pod evictions", vpa.VPAName, vpa.UpdateMode),
			"Alternatively, set updateMode to 'Off' or 'Recommender' to disable active resource management",
		}
	case VPAConflictWarning:
		return []string{
			fmt.Sprintf("Consider changing VPA %q updateMode to 'Off' for safest coexistence with HPA", vpa.VPAName),
		}
	default:
		return nil
	}
}
