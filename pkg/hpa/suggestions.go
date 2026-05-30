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
			suggestions = append(suggestions, Suggestion{
				Title:       "Raise maxReplicas",
				Description: fmt.Sprintf("The HPA is capped at maxReplicas=%d. Raising it to %d allows the controller to add capacity if metrics still require it.", hpa.Spec.MaxReplicas, nextMax),
				Command:     kubectlPatchCommand(hpa, patch),
				Patch:       patch,
				Risk:        "medium",
				Apply:       true,
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
					Command:     kubectlPatchCommand(hpa, patch),
					Patch:       patch,
					Risk:        "medium",
					Apply:       true,
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
				Command:     kubectlPatchCommand(hpa, patch),
				Patch:       patch,
				Risk:        "medium",
				Apply:       true,
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

func kubectlPatchCommand(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string) string {
	return fmt.Sprintf("kubectl patch hpa %s -n %s --type=merge -p '%s'", hpa.Name, hpa.Namespace, patch)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
