package hpa

import (
	"encoding/json"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// LintAutoFix describes a proposed auto-fix for a lint finding.
type LintAutoFix struct {
	// Patch is a JSON merge patch string that would fix the issue.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Command is a kubectl patch command to apply the fix.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Before describes the current state in human-readable form.
	Before string `json:"before,omitempty" yaml:"before,omitempty"`
	// After describes the desired state in human-readable form.
	After string `json:"after,omitempty" yaml:"after,omitempty"`
	// Risk indicates the risk level of applying this fix.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
}

// generateAutoFix produces an auto-fix proposal for a given lint rule and HPA.
// Returns nil if the rule is not auto-fixable.
func generateAutoFix(rule string, hpa *autoscalingv2.HorizontalPodAutoscaler) *LintAutoFix {
	switch rule {
	case "behavior-scaledown":
		return fixMissingScaleDownBehavior(hpa)
	case "target-utilization":
		return fixHighUtilizationTarget(hpa)
	case "tolerance":
		return fixTightTolerance(hpa)
	case "stabilization-window":
		return fixLongStabilizationWindow(hpa)
	default:
		return nil
	}
}

// fixMissingScaleDownBehavior generates a patch adding scaleDown behavior.
func fixMissingScaleDownBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) *LintAutoFix {
	patch := map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
					"policies": []map[string]any{
						{
							"type":           "Percent",
							"value":          50,
							"periodSeconds":  60,
						},
					},
				},
			},
		},
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil
	}

	name := hpa.Name
	ns := hpa.Namespace
	cmd := fmt.Sprintf("kubectl patch hpa %s -n %s --type merge -p '%s'", name, ns, string(patchJSON))

	return &LintAutoFix{
		Patch:   string(patchJSON),
		Command: cmd,
		Before:  "No scaleDown behavior configured",
		After:   "scaleDown with 300s stabilization + 50%/60s policy",
		Risk:    "Low — adds guardrails to prevent aggressive downscaling",
	}
}

// fixHighUtilizationTarget generates a patch lowering the utilization target to 80%.
func fixHighUtilizationTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) *LintAutoFix {
	var currentUtil int32
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType && spec.Resource != nil {
			if spec.Resource.Target.Type == autoscalingv2.UtilizationMetricType && spec.Resource.Target.AverageUtilization != nil {
				currentUtil = *spec.Resource.Target.AverageUtilization
				break
			}
		}
	}
	if currentUtil == 0 {
		return nil
	}

	patch := map[string]any{
		"spec": map[string]any{
			"metrics": []map[string]any{
				{
					"type": "Resource",
					"resource": map[string]any{
						"name": "cpu",
						"target": map[string]any{
							"type":               "Utilization",
							"averageUtilization": 80,
						},
					},
				},
			},
		},
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil
	}

	name := hpa.Name
	ns := hpa.Namespace
	cmd := fmt.Sprintf("kubectl patch hpa %s -n %s --type merge -p '%s'", name, ns, string(patchJSON))

	return &LintAutoFix{
		Patch:   string(patchJSON),
		Command: cmd,
		Before:  fmt.Sprintf("%d%%", currentUtil),
		After:   "80%",
		Risk:    "Medium — changes scaling trigger point",
	}
}

// fixTightTolerance generates a patch setting tolerance to 0.1 (10%).
func fixTightTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler) *LintAutoFix {
	var currentVal string
	var direction string
	patch := map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{},
		},
	}

	behavior := patch["spec"].(map[string]any)["behavior"].(map[string]any)

	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil {
		currentVal = fmt.Sprintf("%.2f%%", hpa.Spec.Behavior.ScaleUp.Tolerance.AsApproximateFloat64()*100)
		direction = "scaleUp"
		behavior["scaleUp"] = map[string]any{
			"tolerance": "0.1",
		}
	} else if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil {
		currentVal = fmt.Sprintf("%.2f%%", hpa.Spec.Behavior.ScaleDown.Tolerance.AsApproximateFloat64()*100)
		direction = "scaleDown"
		behavior["scaleDown"] = map[string]any{
			"tolerance": "0.1",
		}
	} else {
		return nil
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil
	}

	name := hpa.Name
	ns := hpa.Namespace
	cmd := fmt.Sprintf("kubectl patch hpa %s -n %s --type merge -p '%s'", name, ns, string(patchJSON))

	return &LintAutoFix{
		Patch:   string(patchJSON),
		Command: cmd,
		Before:  fmt.Sprintf("%s tolerance: %s", direction, currentVal),
		After:   fmt.Sprintf("%s tolerance: 0.1 (10%%)", direction),
		Risk:    "Medium — widens the no-scale band",
	}
}

// fixLongStabilizationWindow generates a patch reducing the window to 300s (5m).
func fixLongStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *LintAutoFix {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window == nil {
		return nil
	}

	patch := map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
				},
			},
		},
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil
	}

	name := hpa.Name
	ns := hpa.Namespace
	cmd := fmt.Sprintf("kubectl patch hpa %s -n %s --type merge -p '%s'", name, ns, string(patchJSON))

	return &LintAutoFix{
		Patch:   string(patchJSON),
		Command: cmd,
		Before:  fmt.Sprintf("%ds", *window),
		After:   "300s (5m)",
		Risk:    "Low — reduces cooldown delay",
	}
}
