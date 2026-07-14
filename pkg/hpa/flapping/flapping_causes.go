package flapping

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// diagnoseFlappingCauses identifies likely root causes of the observed
// flapping.
func diagnoseFlappingCauses(hpa *autoscalingv2.HorizontalPodAutoscaler, rescales []event.RescaleData) []Cause {
	var causes []Cause

	// Check for tight target: if rescales involve small replica deltas (1-2),
	// the metric is likely hovering near the target threshold.
	if isTightTarget(rescales) {
		causes = append(causes, Cause{
			Type:        "tight-target",
			Description: "Metric values are oscillating around the target threshold, causing the HPA to repeatedly scale up and down by small amounts.",
			Confidence:  "medium",
		})
	}

	// Check for short stabilization window.
	window := currentStabilizationWindowSeconds(hpa)
	if window < 300 {
		causes = append(causes, Cause{
			Type:        "short-stabilization-window",
			Description: fmt.Sprintf("The scaleDown stabilizationWindowSeconds is %ds (< 300s default). A short window allows the HPA to reverse scaling decisions before metrics stabilize.", window),
			Confidence:  "high",
		})
	}

	// Check for missing scaleDown policies.
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil || len(hpa.Spec.Behavior.ScaleDown.Policies) == 0 {
		causes = append(causes, Cause{
			Type:        "missing-scaledown-policy",
			Description: "No explicit scaleDown policies are configured. The controller uses default behavior which may allow rapid downscaling followed by immediate upscaling.",
			Confidence:  "high",
		})
	}

	return causes
}

// isTightTarget infers whether the metric is hovering near the target by
// checking if rescale events involve small replica deltas (1-2 pods).
func isTightTarget(rescales []event.RescaleData) bool {
	if len(rescales) < 3 {
		return false
	}

	smallDeltas := 0
	for i := 1; i < len(rescales); i++ {
		delta := rescales[i].NewSize - rescales[i-1].NewSize
		if delta < 0 {
			delta = -delta
		}
		if delta <= 2 {
			smallDeltas++
		}
	}

	// If more than half the deltas are small, the metric is likely tight.
	return smallDeltas > (len(rescales)-1)/2
}

// generateFlappingFixes produces actionable recommendations based on the
// diagnosed causes.
func generateFlappingFixes(hpa *autoscalingv2.HorizontalPodAutoscaler, causes []Cause) []Fix {
	if len(causes) == 0 {
		return nil
	}

	var fixes []Fix

	for _, cause := range causes {
		switch cause.Type {
		case "tight-target":
			fixes = append(fixes, Fix{
				Action:    "Increase the scaleDown tolerance or widen the target range",
				Rationale: "A wider tolerance band around the target prevents the HPA from reacting to small metric fluctuations near the threshold.",
			})

		case "short-stabilization-window":
			currentWindow := currentStabilizationWindowSeconds(hpa)
			recommendedWindow := currentWindow * 2
			if recommendedWindow < 300 {
				recommendedWindow = 300
			}
			patch := util.MustMarshalJSON(map[string]any{
				"spec": map[string]any{
					"behavior": map[string]any{
						"scaleDown": map[string]any{
							"stabilizationWindowSeconds": recommendedWindow,
						},
					},
				},
			})
			fixes = append(fixes, Fix{
				Action:    fmt.Sprintf("Increase scaleDown stabilizationWindowSeconds from %ds to %ds", currentWindow, recommendedWindow),
				Patch:     patch,
				Rationale: "A longer stabilization window gives the HPA more time to observe sustained metric changes before reversing a scaling decision.",
			})

		case "missing-scaledown-policy":
			window := currentStabilizationWindowSeconds(hpa)
			patch := util.MustMarshalJSON(map[string]any{
				"spec": map[string]any{
					"behavior": map[string]any{
						"scaleDown": map[string]any{
							"stabilizationWindowSeconds": window,
							"selectPolicy":               "Max",
							"policies": []map[string]any{
								{"type": "Percent", "value": 50, "periodSeconds": 60},
							},
						},
					},
				},
			})
			fixes = append(fixes, Fix{
				Action:    "Add explicit scaleDown policy (50%/60s)",
				Patch:     patch,
				Rationale: "Explicit scaleDown policies bound the rate of replica removal, preventing rapid downscale followed by immediate upscale cycles.",
			})
		}
	}

	return fixes
}
