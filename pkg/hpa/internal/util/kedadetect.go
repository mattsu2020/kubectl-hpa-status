// Package util holds small, dependency-free helpers shared across the pkg/hpa
// analysis domains.
package util

import (
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
