// Package util holds small, dependency-free helpers shared across the pkg/hpa
// analysis domains: KEDA-detection heuristics, JSON patch marshalling, kubectl
// patch command formatting, and HPA scaling-policy presence checks. These were
// lifted out of suggestion_rules.go and interpret.go so that leaf sub-packages
// (audit, etc.) can use them without reaching back into the analysis core,
// which would create an import cycle.
package util

import (
	"encoding/json"
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// LooksLikeKEDAManaged reports whether an HPA appears to be owned by KEDA,
// based on conventional label/annotation signals and naming patterns. This is
// a heuristic; the authoritative check is a ScaledObject CRD lookup when the
// KEDA API is available.
func LooksLikeKEDAManaged(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if hpa == nil {
		return false
	}
	// Strong signals first: official keda.sh key prefix or exact managed-by.
	if HasKEDAKeySignal(hpa.Labels) || HasKEDAKeySignal(hpa.Annotations) {
		return true
	}

	// Medium signal: conventional KEDA HPA name prefix.
	if strings.HasPrefix(hpa.Name, "keda-hpa-") {
		return true
	}

	// Weak fallback: any value mentioning "keda".
	if HasKEDAValueFallback(hpa.Labels) || HasKEDAValueFallback(hpa.Annotations) {
		return true
	}

	return false
}

// HasKEDAKeySignal reports whether a label/annotation map carries an official
// keda.sh key prefix or the managed-by=keda pair.
func HasKEDAKeySignal(m map[string]string) bool {
	for key, value := range m {
		lk := strings.ToLower(key)
		if strings.Contains(lk, "keda.sh/") {
			return true
		}
		if lk == "app.kubernetes.io/managed-by" && strings.EqualFold(value, "keda") {
			return true
		}
	}
	return false
}

// HasKEDAValueFallback reports whether any value contains the substring
// "keda". This is a weak, false-positive-prone signal used only as a last
// resort.
func HasKEDAValueFallback(m map[string]string) bool {
	for _, value := range m {
		if strings.Contains(strings.ToLower(value), "keda") {
			return true
		}
	}
	return false
}

// MarshalJSON serialises a value to a JSON string, returning "{}" on error.
// Callers always pass internal map[string]any literals built from HPA fields,
// so a marshal failure is not expected; returning an empty JSON object avoids
// crashing the CLI on a single bad value.
func MarshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// KubectlPatchCommand formats a kubectl patch command string for an HPA with
// the given JSON merge patch, including --dry-run=server.
func KubectlPatchCommand(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string) string {
	command := fmt.Sprintf("kubectl patch hpa %s -n %s --type=merge -p '%s'", hpa.Name, hpa.Namespace, patch)
	command += " --dry-run=server"
	return command
}

// MissingPolicies reports whether the given behavior lacks scaling policies
// for the specified direction ("scaleUp" or "scaleDown").
func MissingPolicies(behavior *autoscalingv2.HorizontalPodAutoscalerBehavior, direction string) bool {
	if behavior == nil {
		return true
	}
	var rules *autoscalingv2.HPAScalingRules
	switch direction {
	case "scaleUp":
		rules = behavior.ScaleUp
	case "scaleDown":
		rules = behavior.ScaleDown
	default:
		return true
	}
	if rules == nil {
		return true
	}
	return len(rules.Policies) == 0
}
