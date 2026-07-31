package enrichment

import hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"

// Source aliases the canonical enrichment source.
// Internal enrichment and public analysis output use one canonical status
// model. These aliases retain the concise package-local vocabulary without
// mirroring fields or enum values by hand.
type Source = hpaanalysis.EnrichmentSource

const ( //nolint:revive // Aliased constants retain their canonical exported names.
	// SourceKEDA identifies KEDA enrichment.
	SourceKEDA = hpaanalysis.EnrichmentSourceKEDA
	// SourceVPA identifies VPA enrichment.
	SourceVPA = hpaanalysis.EnrichmentSourceVPA
)

// State aliases the canonical enrichment state.
type State = hpaanalysis.EnrichmentState

const ( //nolint:revive // Aliased constants retain their canonical exported names.
	// StateActive means enrichment completed and contributed data.
	StateActive      = hpaanalysis.EnrichmentStateActive
	StateSkipped     = hpaanalysis.EnrichmentStateSkipped     //nolint:revive // Canonical alias.
	StateDisabled    = hpaanalysis.EnrichmentStateDisabled    //nolint:revive // Canonical alias.
	StateUnavailable = hpaanalysis.EnrichmentStateUnavailable //nolint:revive // Canonical alias.
	StateError       = hpaanalysis.EnrichmentStateError       //nolint:revive // Canonical alias.
)

// Entry aliases a canonical enrichment status entry.
type Entry = hpaanalysis.EnrichmentStatusEntry

// Status aliases the canonical aggregate enrichment status.
type Status = hpaanalysis.EnrichmentStatus
