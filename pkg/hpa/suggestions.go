package hpa

import (
	"encoding/json"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func BuildSuggestions(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Suggestion {
	var suggestions []Suggestion
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		suggestions = append(suggestions, Suggestion{
			Title:       "Restore metric availability",
			Description: "ScalingActive is not True. Fix metrics-server or the custom/external metrics adapter before changing HPA limits.",
			Risk:        "low",
		})
		return suggestions
	}

	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			nextMax := recommendedMaxReplicas(hpa)
			patch := mustJSON(map[string]any{"spec": map[string]any{"maxReplicas": nextMax}})
			warnings := []string{
				"Confirm node capacity, PodDisruptionBudgets, quotas, and downstream dependency limits before persisting this change.",
				"Run the patch as a server-side dry-run first; the plugin also dry-runs by default when --apply is used.",
			}
			if !hasVisibleScaleUpPressure(hpa) {
				warnings = append(warnings, "No visible resource metric is above target; another metric or controller behavior may be responsible, so review currentMetrics before raising maxReplicas.")
			}
			suggestions = append(suggestions, Suggestion{
				Title:       "Raise maxReplicas",
				Description: fmt.Sprintf("The HPA is capped at maxReplicas=%d. Raising it to %d allows the controller to add capacity if metrics still require it.", hpa.Spec.MaxReplicas, nextMax),
				Command:     kubectlPatchCommand(hpa, patch, true),
				Patch:       patch,
				Risk:        "medium",
				Preconditions: []string{
					"ScalingActive is True.",
					"ScalingLimited is True and desiredReplicas equals maxReplicas.",
					"Workload and cluster capacity can tolerate the proposed replica ceiling.",
				},
				Warnings: warnings,
				Apply:    true,
			})
		case minReplicas:
			nextMin := minReplicas - 1
			if nextMin < 1 {
				nextMin = 1
			}
			if nextMin < minReplicas {
				patch := mustJSON(map[string]any{"spec": map[string]any{"minReplicas": nextMin}})
				suggestions = append(suggestions, Suggestion{
					Title:       "Lower minReplicas",
					Description: fmt.Sprintf("The HPA is capped at minReplicas=%d. Lowering it to %d allows further scale-down.", minReplicas, nextMin),
					Command:     kubectlPatchCommand(hpa, patch, true),
					Patch:       patch,
					Risk:        "medium",
					Preconditions: []string{
						"ScalingActive is True.",
						"ScalingLimited is True and desiredReplicas equals minReplicas.",
						"The workload can safely run at the proposed lower minimum.",
					},
					Warnings: []string{"Validate availability, cold-start behavior, and disruption budgets before persisting this change."},
					Apply:    true,
				})
			}
		}
	}

	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		if window := scaleDownStabilizationWindow(hpa); window != nil && *window > 60 {
			nextWindow := *window / 2
			patch := mustJSON(map[string]any{
				"spec": map[string]any{
					"behavior": map[string]any{
						"scaleDown": map[string]any{"stabilizationWindowSeconds": nextWindow},
					},
				},
			})
			suggestions = append(suggestions, Suggestion{
				Title:       "Shorten scale-down stabilization",
				Description: fmt.Sprintf("Scale-down is stabilized for up to %ds. Reducing the window to %ds makes scale-down respond sooner.", *window, nextWindow),
				Command:     kubectlPatchCommand(hpa, patch, true),
				Patch:       patch,
				Risk:        "medium",
				Preconditions: []string{
					"AbleToScale reason reports ScaleDownStabilized.",
					"The workload can tolerate faster downscale decisions.",
				},
				Warnings: []string{"Shorter stabilization can increase replica churn when traffic is bursty."},
				Apply:    true,
			})
		}
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, Suggestion{
			Title:       "No safe automatic fix",
			Description: "No concrete HPA spec patch is suggested from current status. Inspect metrics, Events, and workload capacity before changing targets or limits.",
			Risk:        "low",
		})
	}
	return suggestions
}

func recommendedMaxReplicas(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	next := hpa.Spec.MaxReplicas * 2
	if hpa.Status.DesiredReplicas > next {
		next = hpa.Status.DesiredReplicas
	}
	if next <= hpa.Spec.MaxReplicas {
		next = hpa.Spec.MaxReplicas + 1
	}
	return next
}

func hasVisibleScaleUpPressure(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range hpa.Status.CurrentMetrics {
		formatted := FormatMetricStatus(hpa, metric)
		if formatted.Ratio != nil && *formatted.Ratio > 1 {
			return true
		}
	}
	return false
}

func kubectlPatchCommand(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string, dryRun bool) string {
	command := fmt.Sprintf("kubectl patch hpa %s -n %s --type=merge -p '%s'", hpa.Name, hpa.Namespace, patch)
	if dryRun {
		command += " --dry-run=server"
	}
	return command
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
