package kube

import (
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// KEDADetectionSource identifies how KEDA management was detected.
type KEDADetectionSource string

const (
	// KEDADetectionLabel indicates detection via keda.sh label.
	KEDADetectionLabel KEDADetectionSource = "label"
	// KEDADetectionAnnotation indicates detection via keda.sh annotation.
	KEDADetectionAnnotation KEDADetectionSource = "annotation"
	// KEDADetectionNamePrefix indicates detection via keda-hpa- name prefix.
	KEDADetectionNamePrefix KEDADetectionSource = "name-prefix"
	// KEDADetectionScaledObject indicates confirmed detection via ScaledObject CRD match.
	KEDADetectionScaledObject KEDADetectionSource = "scaledobject"
)

// KEDADetectionConfidence represents the reliability of KEDA detection.
type KEDADetectionConfidence string

const (
	// KEDAConfidenceHigh indicates ScaledObject CRD was found (authoritative).
	KEDAConfidenceHigh KEDADetectionConfidence = "high"
	// KEDAConfidenceMedium indicates label/annotation match (likely correct).
	KEDAConfidenceMedium KEDADetectionConfidence = "medium"
	// KEDAConfidenceLow indicates name-prefix match only (heuristic, may be false positive).
	KEDAConfidenceLow KEDADetectionConfidence = "low"
)

// KEDADetectionResult holds the outcome of KEDA detection for an HPA.
type KEDADetectionResult struct {
	Managed    bool                    `json:"managed" yaml:"managed"`
	Source     KEDADetectionSource     `json:"source,omitempty" yaml:"source,omitempty"`
	Confidence KEDADetectionConfidence `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Name       string                  `json:"scaledObjectName,omitempty" yaml:"scaledObjectName,omitempty"`
}

// DetectKEDA checks whether an HPA is KEDA-managed by inspecting labels,
// annotations, and name prefix. Returns a KEDADetectionResult with the
// detection source, confidence level, and extracted ScaledObject name.
//
// Detection is ordered by confidence to reduce false positives:
//   - Strong (medium confidence): a label/annotation key with the official
//     "keda.sh/" prefix, or the canonical "app.kubernetes.io/managed-by"
//     key whose value is "keda" or starts with "keda" (e.g. "keda-operator").
//   - Medium (low confidence): the conventional "keda-hpa-" name prefix.
//   - Weak fallback: any remaining label/annotation value that merely
//     contains the substring "keda". This catches unusual operator annotations
//     but is the most false-positive-prone signal, so it is evaluated last.
func DetectKEDA(hpa *autoscalingv2.HorizontalPodAutoscaler) KEDADetectionResult {
	if hpa == nil {
		return KEDADetectionResult{}
	}

	// Strong signals from labels and annotations.
	if hasKEDAKeySignal(hpa.Labels) {
		return KEDADetectionResult{
			Managed:    true,
			Source:     KEDADetectionLabel,
			Confidence: KEDAConfidenceMedium,
			Name:       extractScaledObjectName(hpa),
		}
	}
	if hasKEDAKeySignal(hpa.Annotations) {
		return KEDADetectionResult{
			Managed:    true,
			Source:     KEDADetectionAnnotation,
			Confidence: KEDAConfidenceMedium,
			Name:       extractScaledObjectName(hpa),
		}
	}

	// Name prefix: conventional KEDA HPA naming.
	if strings.HasPrefix(hpa.Name, "keda-hpa-") {
		return KEDADetectionResult{
			Managed:    true,
			Source:     KEDADetectionNamePrefix,
			Confidence: KEDAConfidenceLow,
			Name:       extractScaledObjectName(hpa),
		}
	}

	// Weak fallback: a value mentioning "keda" when no stronger signal fired.
	if hasKEDAValueFallback(hpa.Labels) {
		return KEDADetectionResult{
			Managed:    true,
			Source:     KEDADetectionLabel,
			Confidence: KEDAConfidenceLow,
			Name:       extractScaledObjectName(hpa),
		}
	}
	if hasKEDAValueFallback(hpa.Annotations) {
		return KEDADetectionResult{
			Managed:    true,
			Source:     KEDADetectionAnnotation,
			Confidence: KEDAConfidenceLow,
			Name:       extractScaledObjectName(hpa),
		}
	}
	return KEDADetectionResult{}
}

// hasKEDAKeySignal reports whether any key uses the official keda.sh prefix or
// the canonical managed-by key is set to a keda value (exact or keda-prefixed,
// e.g. "keda" or "keda-operator").
func hasKEDAKeySignal(m map[string]string) bool {
	for key, value := range m {
		lk := strings.ToLower(key)
		if strings.Contains(lk, "keda.sh/") {
			return true
		}
		if lk == "app.kubernetes.io/managed-by" {
			lv := strings.ToLower(strings.TrimSpace(value))
			if lv == "keda" || strings.HasPrefix(lv, "keda") {
				return true
			}
		}
	}
	return false
}

// hasKEDAValueFallback reports whether any value contains the substring "keda".
// This is a weak, false-positive-prone signal used only as a last resort.
func hasKEDAValueFallback(m map[string]string) bool {
	for _, value := range m {
		if strings.Contains(strings.ToLower(value), "keda") {
			return true
		}
	}
	return false
}
