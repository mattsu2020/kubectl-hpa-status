package hpa

// ---------------------------------------------------------------------------
// Support Bundle types (support-bundle command)
// ---------------------------------------------------------------------------

// SupportBundleMetadata holds metadata about what's included in the bundle.
type SupportBundleMetadata struct {
	// KEDADetected indicates whether KEDA was detected in the cluster.
	KEDADetected bool `json:"kedaDetected" yaml:"kedaDetected"`
	// VPADetected indicates whether VPA was detected in the cluster.
	VPADetected bool `json:"vpaDetected" yaml:"vpaDetected"`
	// KEDAScaledObject is the KEDA ScaledObject YAML if KEDA is detected.
	KEDAScaledObject string `json:"kedaScaledObject,omitempty" yaml:"kedaScaledObject,omitempty"`
	// VPARecommendation is the VPA recommendation YAML if VPA is detected.
	VPARecommendation string `json:"vpaRecommendation,omitempty" yaml:"vpaRecommendation,omitempty"`
}
