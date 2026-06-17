package enrichment

import hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"

// Source identifies which enrichment system produced a status entry.
type Source string

const (
	// SourceKEDA indicates KEDA ScaledObject enrichment.
	SourceKEDA Source = "keda"
	// SourceVPA indicates VerticalPodAutoscaler enrichment.
	SourceVPA Source = "vpa"
)

// State describes the outcome of an enrichment operation.
type State string

const (
	// StateActive means enrichment data was successfully retrieved.
	StateActive State = "active"
	// StateSkipped means the HPA was not relevant for this enrichment.
	StateSkipped State = "skipped"
	// StateDisabled means the enrichment source was not requested.
	StateDisabled State = "disabled"
	// StateUnavailable means the required CRD is not installed.
	StateUnavailable State = "unavailable"
	// StateError means enrichment failed due to an error.
	StateError State = "error"
)

// Entry records the outcome for a single enrichment source.
type Entry struct {
	Source Source `json:"source" yaml:"source"`
	State  State  `json:"state" yaml:"state"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// Status holds the enrichment outcomes for all sources.
// Attached to Analysis for visibility in --debug and -o json output.
type Status struct {
	KEDA *Entry `json:"keda,omitempty" yaml:"keda,omitempty"`
	VPA  *Entry `json:"vpa,omitempty" yaml:"vpa,omitempty"`
}

// KEDAEntry returns the KEDA enrichment entry, or a disabled default.
func (s *Status) KEDAEntry() Entry {
	if s != nil && s.KEDA != nil {
		return *s.KEDA
	}
	return Entry{Source: SourceKEDA, State: StateDisabled}
}

// VPAEntry returns the VPA enrichment entry, or a disabled default.
func (s *Status) VPAEntry() Entry {
	if s != nil && s.VPA != nil {
		return *s.VPA
	}
	return Entry{Source: SourceVPA, State: StateDisabled}
}

// ToAnalysisStatus converts the internal Status into the JSON-mirror type
// defined in pkg/hpa. This keeps pkg/hpa free of any dependency on this
// internal package while preserving the serialised contract.
func (s Status) ToAnalysisStatus() *hpaanalysis.EnrichmentStatus {
	out := &hpaanalysis.EnrichmentStatus{}
	if s.KEDA != nil {
		out.KEDA = &hpaanalysis.EnrichmentStatusEntry{
			Source: hpaanalysis.EnrichmentSource(s.KEDA.Source),
			State:  hpaanalysis.EnrichmentState(s.KEDA.State),
			Reason: s.KEDA.Reason,
		}
	}
	if s.VPA != nil {
		out.VPA = &hpaanalysis.EnrichmentStatusEntry{
			Source: hpaanalysis.EnrichmentSource(s.VPA.Source),
			State:  hpaanalysis.EnrichmentState(s.VPA.State),
			Reason: s.VPA.Reason,
		}
	}
	return out
}
