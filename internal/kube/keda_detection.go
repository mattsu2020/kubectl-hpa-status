package kube

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
