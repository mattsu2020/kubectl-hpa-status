package enrichment

// EnrichmentSource identifies which enrichment system produced a status entry.
type EnrichmentSource string

const (
	// EnrichmentSourceKEDA indicates KEDA ScaledObject enrichment.
	EnrichmentSourceKEDA EnrichmentSource = "keda"
	// EnrichmentSourceVPA indicates VerticalPodAutoscaler enrichment.
	EnrichmentSourceVPA EnrichmentSource = "vpa"
)

// EnrichmentState describes the outcome of an enrichment operation.
type EnrichmentState string

const (
	// EnrichmentStateActive means enrichment data was successfully retrieved.
	EnrichmentStateActive EnrichmentState = "active"
	// EnrichmentStateSkipped means the HPA was not relevant for this enrichment.
	EnrichmentStateSkipped EnrichmentState = "skipped"
	// EnrichmentStateDisabled means the enrichment source was not requested.
	EnrichmentStateDisabled EnrichmentState = "disabled"
	// EnrichmentStateUnavailable means the required CRD is not installed.
	EnrichmentStateUnavailable EnrichmentState = "unavailable"
	// EnrichmentStateError means enrichment failed due to an error.
	EnrichmentStateError EnrichmentState = "error"
)

// EnrichmentEntry records the outcome for a single enrichment source.
type EnrichmentEntry struct {
	Source EnrichmentSource `json:"source" yaml:"source"`
	State  EnrichmentState  `json:"state" yaml:"state"`
	Reason string           `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// EnrichmentStatus holds the enrichment outcomes for all sources.
// Attached to Analysis for visibility in --debug and -o json output.
type EnrichmentStatus struct {
	KEDA *EnrichmentEntry `json:"keda,omitempty" yaml:"keda,omitempty"`
	VPA  *EnrichmentEntry `json:"vpa,omitempty" yaml:"vpa,omitempty"`
}

// KEDAEntry returns the KEDA enrichment entry, or a disabled default.
func (s *EnrichmentStatus) KEDAEntry() EnrichmentEntry {
	if s != nil && s.KEDA != nil {
		return *s.KEDA
	}
	return EnrichmentEntry{Source: EnrichmentSourceKEDA, State: EnrichmentStateDisabled}
}

// VPAEntry returns the VPA enrichment entry, or a disabled default.
func (s *EnrichmentStatus) VPAEntry() EnrichmentEntry {
	if s != nil && s.VPA != nil {
		return *s.VPA
	}
	return EnrichmentEntry{Source: EnrichmentSourceVPA, State: EnrichmentStateDisabled}
}
