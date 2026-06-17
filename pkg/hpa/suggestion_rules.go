package hpa

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SuggestionRule is a function that examines an HPA and returns concrete
// suggestions. Rules are independent: each rule inspects specific conditions
// or state and returns zero or more suggestions. Unlike diagnostic rules,
// suggestion rules do not participate in early termination.
type SuggestionRule func(ctx SuggestionContext) []Suggestion

// coreSuggestionRules returns the ordered list of rules that replace the
// monolithic BuildSuggestions function. Rule order matters: rules that
// check fundamental conditions (ScalingActive, ScalingLimited) run first.
func coreSuggestionRules() []SuggestionRule {
	return []SuggestionRule{
		scalingActiveRule,
		scalingLimitedMaxRule,
		scalingLimitedMinRule,
		scaleDownStabilizedRule,
		behaviorPolicyRule,
		toleranceRule,
		metricMixRule,
		kedaRule,
	}
}

// scalingActiveRule generates suggestions when ScalingActive is not True.
func scalingActiveRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	if condition := FindCondition(hpa, ConditionScalingActive); condition != nil && condition.Status != corev1.ConditionTrue {
		suggestions := []Suggestion{{
			Title:       "Restore metric availability",
			Description: "ScalingActive is not True. Fix metrics-server or the custom/external metrics adapter before changing HPA limits.",
			Risk:        "low",
		}}
		return append(suggestions, staleMetricSuggestions(hpa)...)
	}
	return nil
}

// scalingLimitedMaxRule suggests raising maxReplicas when HPA is capped at max.
func scalingLimitedMaxRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	condition := FindCondition(hpa, ConditionScalingLimited)
	if condition == nil || condition.Status != corev1.ConditionTrue {
		return nil
	}
	if hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas {
		return nil
	}
	// Only suggest raising maxReplicas when the workload is actively running.
	if hpa.Status.CurrentReplicas == 0 {
		return nil
	}

	nextMax := recommendedMaxReplicas(hpa)
	patch := marshalJSON(map[string]any{"spec": map[string]any{"maxReplicas": nextMax}})
	warnings := []string{
		"Confirm node capacity, PodDisruptionBudgets, quotas, and downstream dependency limits before persisting this change.",
		"Run the patch as a server-side dry-run first; the plugin also dry-runs by default when --apply is used.",
	}
	if !hasVisibleScaleUpPressure(hpa) {
		warnings = append(warnings, "No visible resource metric is above target; another metric or controller behavior may be responsible, so review currentMetrics before raising maxReplicas.")
	}

	// Add capacity warnings when available.
	if ctx.Capacity != nil && ctx.Capacity.Insufficient {
		warnings = append(warnings, fmt.Sprintf("CAPACITY WARNING: %s Raising maxReplicas to %d may result in Pending pods.", ctx.Capacity.Reason, nextMax))
	}

	return []Suggestion{{
		Title:       "Raise maxReplicas",
		Description: fmt.Sprintf("The HPA is capped at maxReplicas=%d. Raising it to %d allows the controller to add capacity if metrics still require it.", hpa.Spec.MaxReplicas, nextMax),
		Command:     kubectlPatchCommand(hpa, patch),
		Patch:       patch,
		Risk:        "medium",
		Preconditions: []string{
			"ScalingActive is True.",
			"ScalingLimited is True and desiredReplicas equals maxReplicas.",
			"currentReplicas > 0 (HPA is not in an inactive or scale-to-zero state).",
			"Workload and cluster capacity can tolerate the proposed replica ceiling.",
		},
		Warnings: warnings,
		Apply:    true,
	}}
}

// scalingLimitedMinRule suggests lowering minReplicas when HPA is at min.
func scalingLimitedMinRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	minReplicas := ctx.MinReplicas
	condition := FindCondition(hpa, ConditionScalingLimited)
	if condition == nil || condition.Status != corev1.ConditionTrue {
		return nil
	}
	if hpa.Status.DesiredReplicas != minReplicas {
		return nil
	}
	// Only suggest lowering minReplicas when there is room to lower.
	if minReplicas <= 1 {
		return nil
	}

	nextMin := minReplicas - 1
	if nextMin < 1 {
		nextMin = 1
	}
	if nextMin >= minReplicas {
		return nil
	}

	patch := marshalJSON(map[string]any{"spec": map[string]any{"minReplicas": nextMin}})
	return []Suggestion{{
		Title:       "Lower minReplicas",
		Description: fmt.Sprintf("The HPA is capped at minReplicas=%d. Lowering it to %d allows further scale-down.", minReplicas, nextMin),
		Command:     kubectlPatchCommand(hpa, patch),
		Patch:       patch,
		Risk:        "medium",
		Preconditions: []string{
			"ScalingActive is True.",
			"ScalingLimited is True and desiredReplicas equals minReplicas.",
			"minReplicas > 1 (there is room to lower).",
			"The workload can safely run at the proposed lower minimum.",
		},
		Warnings: []string{"Validate availability, cold-start behavior, and disruption budgets before persisting this change."},
		Apply:    true,
	}}
}

// scaleDownStabilizedRule suggests shortening scale-down stabilization window.
func scaleDownStabilizedRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	condition := FindCondition(hpa, ConditionAbleToScale)
	if condition == nil || condition.Reason != "ScaleDownStabilized" {
		return nil
	}

	window := scaleDownStabilizationWindow(hpa)
	if window == nil || *window <= 60 || *window == 300 {
		return nil
	}

	nextWindow := *window / 2
	patch := marshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{"stabilizationWindowSeconds": nextWindow},
			},
		},
	})

	return []Suggestion{{
		Title:       "Shorten scale-down stabilization",
		Description: fmt.Sprintf("Scale-down is stabilized for up to %ds. Reducing the window to %ds makes scale-down respond sooner.", *window, nextWindow),
		Command:     kubectlPatchCommand(hpa, patch),
		Patch:       patch,
		Risk:        "medium",
		Preconditions: []string{
			"AbleToScale reason reports ScaleDownStabilized.",
			"The stabilization window is not at the default 300s (it was explicitly set).",
			"The workload can tolerate faster downscale decisions.",
		},
		Warnings: []string{"Shorter stabilization can increase replica churn when traffic is bursty."},
		Apply:    true,
	}}
}

// behaviorPolicyRule suggests adding explicit scale-up/scale-down policies.
func behaviorPolicyRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	var suggestions []Suggestion

	if hasVisibleScaleUpPressure(hpa) && missingPolicies(hpa.Spec.Behavior, "scaleUp") {
		patch := marshalJSON(map[string]any{
			"spec": map[string]any{
				"behavior": map[string]any{
					"scaleUp": map[string]any{
						"stabilizationWindowSeconds": 0,
						"selectPolicy":               "Max",
						"policies": []map[string]any{
							{"type": "Percent", "value": 100, "periodSeconds": 60},
							{"type": "Pods", "value": 4, "periodSeconds": 60},
						},
					},
				},
			},
		})
		suggestions = append(suggestions, Suggestion{
			Title:       "Add explicit scale-up policy",
			Description: "Visible metrics are above target and scale-up behavior is implicit. Adding explicit scaleUp policies makes burst response predictable.",
			Command:     kubectlPatchCommand(hpa, patch),
			Patch:       patch,
			Risk:        "medium",
			Preconditions: []string{
				"The workload can absorb faster replica growth.",
				"Cluster autoscaler, quotas, and downstream services can handle the higher ramp rate.",
			},
			Warnings: []string{"Aggressive scale-up can increase cost and amplify traffic spikes."},
			Apply:    true,
		})
	}

	if visibleScaleDownPressure(hpa) && missingPolicies(hpa.Spec.Behavior, "scaleDown") {
		patch := marshalJSON(map[string]any{
			"spec": map[string]any{
				"behavior": map[string]any{
					"scaleDown": map[string]any{
						"stabilizationWindowSeconds": 300,
						"selectPolicy":               "Max",
						"policies": []map[string]any{
							{"type": "Percent", "value": 50, "periodSeconds": 60},
						},
					},
				},
			},
		})
		suggestions = append(suggestions, Suggestion{
			Title:       "Add explicit scale-down policy",
			Description: "Metrics are below target and scale-down behavior is implicit. A bounded scaleDown policy keeps downscale predictable.",
			Command:     kubectlPatchCommand(hpa, patch),
			Patch:       patch,
			Risk:        "medium",
			Preconditions: []string{
				"The workload tolerates gradual downscale.",
				"Traffic has enough signal stability for a 300s stabilization window.",
			},
			Warnings: []string{"Too-fast scale-down can cause latency spikes during rebound traffic."},
			Apply:    true,
		})
	}

	return suggestions
}

// toleranceRule suggests reviewing scale-up tolerance for small sustained pressure.
func toleranceRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	if hpa.Status.CurrentReplicas != hpa.Status.DesiredReplicas {
		return nil
	}

	metric, ok := MetricOutsideTarget(hpa)
	if !ok || metric.Ratio < 1.02 || metric.Ratio > 1.10 {
		return nil
	}

	tolerance := resource.MustParse("0.05")
	patch := marshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleUp": map[string]any{
					"tolerance": tolerance.String(),
				},
			},
		},
	})

	return []Suggestion{{
		Title:       "Review scale-up tolerance",
		Description: fmt.Sprintf("%s is %.3fx target while replicas are unchanged. If your cluster enables HPAConfigurableTolerance, a lower scaleUp tolerance such as 0.05 can make small sustained pressure scale sooner.", metric.Name, metric.Ratio),
		Command:     kubectlPatchCommand(hpa, patch),
		Patch:       patch,
		Risk:        "medium",
		Preconditions: []string{
			"The API server and controller-manager enable HPAConfigurableTolerance.",
			"The signal is sustained, not a short-lived metric spike.",
		},
		Warnings: []string{"Lower tolerance can cause more frequent scaling and replica churn."},
		Apply:    true,
	}}
}

// metricMixRule suggests fixes for stale external metrics and resource safety.
func metricMixRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	var suggestions []Suggestion
	hasResource := false

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType || spec.Type == autoscalingv2.ContainerResourceMetricSourceType {
			hasResource = true
		}
	}

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil {
			if !hasCurrentExternalMetric(hpa, spec.External.Metric.Name, spec.External.Metric.Selector) {
				suggestions = append(suggestions, Suggestion{
					Title:       "Investigate stale external metric",
					Description: fmt.Sprintf("External metric %q is configured but missing from currentMetrics. Fix the adapter/selector or remove the metric if it is no longer used.", spec.External.Metric.Name),
					Risk:        "low",
					Preconditions: []string{
						"The external metric is absent from HPA status for more than one reconciliation interval.",
						"Events or adapter logs confirm the metric cannot be fetched.",
					},
				})
			}
		}
	}

	if !hasResource && len(hpa.Spec.Metrics) > 0 {
		suggestions = append(suggestions, Suggestion{
			Title:       "Consider a resource safety metric",
			Description: "This HPA relies only on custom, object, pods, or external metrics. Adding CPU or memory can provide a safety signal when business metrics are delayed.",
			Risk:        "low",
			Warnings:    []string{"Only add resource metrics when requests are configured correctly and the metric matches the workload bottleneck."},
		})
	}

	return suggestions
}

// kedaRule suggests inspecting KEDA ScaledObject for KEDA-managed HPAs.
func kedaRule(ctx SuggestionContext) []Suggestion {
	hpa := ctx.HPA
	if !looksLikeKEDAManaged(hpa) {
		return nil
	}
	return []Suggestion{{
		Title:       "Inspect KEDA ScaledObject",
		Description: "This HPA appears to be KEDA-managed. Check the owning ScaledObject status, scaler authentication, and keda-operator logs before patching generated HPA behavior directly.",
		Risk:        "low",
		Preconditions: []string{
			"The HPA has KEDA labels/annotations or a keda-hpa-* name.",
			"External metrics are missing, stale, or inconsistent with expected scaler output.",
		},
		Warnings: []string{"Direct HPA patches may be overwritten by KEDA reconciliation."},
	}}
}

func noSafeFixSuggestion() Suggestion {
	return Suggestion{
		Title:       "No safe automatic fix",
		Description: "No concrete HPA spec patch is suggested from current status. Inspect metrics, Events, and workload capacity before changing targets or limits.",
		Risk:        "low",
	}
}

// Helper functions moved from suggestions.go

func staleMetricSuggestions(hpa *autoscalingv2.HorizontalPodAutoscaler) []Suggestion {
	var suggestions []Suggestion
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ExternalMetricSourceType && spec.External != nil {
			suggestions = append(suggestions, Suggestion{
				Title:       "Check external metric freshness",
				Description: fmt.Sprintf("External metric %q can block scaling when the adapter returns stale or missing data. Check the external.metrics.k8s.io API and adapter logs.", spec.External.Metric.Name),
				Risk:        "low",
			})
		}
		if spec.Type == autoscalingv2.ObjectMetricSourceType && spec.Object != nil {
			suggestions = append(suggestions, Suggestion{
				Title:       "Check object metric target",
				Description: fmt.Sprintf("Object metric %q targets %s/%s. Verify that object exists and the adapter reports the same metric name.", spec.Object.Metric.Name, spec.Object.DescribedObject.Kind, spec.Object.DescribedObject.Name),
				Risk:        "low",
			})
		}
	}
	return suggestions
}

func missingPolicies(behavior *autoscalingv2.HorizontalPodAutoscalerBehavior, direction string) bool {
	if behavior == nil {
		return true
	}
	var rules *autoscalingv2.HPAScalingRules
	switch direction {
	case "scaleUp":
		rules = behavior.ScaleUp
	case "scaleDown":
		rules = behavior.ScaleDown
	}
	return rules == nil || len(rules.Policies) == 0
}

func visibleScaleDownPressure(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range hpa.Status.CurrentMetrics {
		formatted := FormatMetricStatus(hpa, metric)
		if formatted.Ratio != nil && *formatted.Ratio < 0.80 && !math.IsNaN(*formatted.Ratio) {
			return true
		}
	}
	return false
}

// recommendedMaxReplicasCap is the absolute upper bound for suggested
// maxReplicas regardless of the doubling formula. This prevents dangerous
// suggestions on production clusters where maxReplicas is already large.
const recommendedMaxReplicasCap int32 = 200

func recommendedMaxReplicas(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	next := hpa.Spec.MaxReplicas * 2
	if hpa.Status.DesiredReplicas > next {
		next = hpa.Status.DesiredReplicas
	}
	if next <= hpa.Spec.MaxReplicas {
		next = hpa.Spec.MaxReplicas + 1
	}
	// Cap to prevent dangerous suggestions on production clusters.
	if next > recommendedMaxReplicasCap {
		next = recommendedMaxReplicasCap
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

// kubectlPatchCommand renders a copy-paste kubectl patch command with
// --dry-run=server attached. Suggestions are intentionally non-destructive:
// the dry-run flag is always present so operators must explicitly strip it to
// apply a change. The now-removed dryRun parameter was always true at every
// call site (see unparam); callers wanting a no-dry-run variant must build a
// different command.
func kubectlPatchCommand(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string) string {
	command := fmt.Sprintf("kubectl patch hpa %s -n %s --type=merge -p '%s'", hpa.Name, hpa.Namespace, patch)
	command += " --dry-run=server"
	return command
}

func marshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		// Callers always pass internal map[string]any literals built from HPA
		// fields, so a marshal failure is not expected. Return an empty JSON
		// object rather than crashing the CLI on a single bad value.
		return "{}"
	}
	return string(data)
}

// SuggestionDiff renders a field-level diff of a suggestion's patch against
// the current HPA spec values.
func SuggestionDiff(currentMin *int32, currentDesired int32, currentMax int32, patch string) string {
	var parsed struct {
		Spec struct {
			MinReplicas *int32 `json:"minReplicas"`
			MaxReplicas *int32 `json:"maxReplicas"`
			Behavior    any    `json:"behavior"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(patch), &parsed); err != nil {
		return fmt.Sprintf("  patch: %s", patch)
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("  status.desiredReplicas: %d (current status, unchanged by patch)", currentDesired))
	if parsed.Spec.MinReplicas != nil {
		current := DefaultMinReplicas
		if currentMin != nil {
			current = *currentMin
		}
		lines = append(lines, fmt.Sprintf("  spec.minReplicas: %d -> %d", current, *parsed.Spec.MinReplicas))
	}
	if parsed.Spec.MaxReplicas != nil {
		lines = append(lines, fmt.Sprintf("  spec.maxReplicas: %d -> %d", currentMax, *parsed.Spec.MaxReplicas))
	}
	if parsed.Spec.Behavior != nil {
		lines = append(lines, "  spec.behavior: updated")
	}
	if len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("  patch: %s", patch))
	}
	return strings.Join(lines, "\n") + "\n"
}
