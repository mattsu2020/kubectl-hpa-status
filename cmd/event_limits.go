package cmd

// Named defaults for "recent events" fetch depths. Each command intentionally
// uses a different budget: quick explanations stay terse, diagnostics that
// correlate events with scaling decisions fetch deeper. Naming them keeps the
// differences explicit instead of looking like drift between magic numbers.
const (
	// explainEventLimit bounds the events shown by --explain on status, where
	// events are supplementary context for the interpretation.
	explainEventLimit = 5
	// contextEventLimit bounds the events in context/doctor summaries that
	// mix events with several other signal sources.
	contextEventLimit = 10
	// scalePathEventLimit bounds the events scanned when reconstructing the
	// recent scale path; the path renderer shows at most a handful of steps.
	scalePathEventLimit = 10
	// diagnosticEventLimit bounds the events scanned by symptom-focused
	// diagnostics (blockers, snapshot) that correlate events with failures.
	diagnosticEventLimit = 20
	// bundleEventLimit bounds the events captured in support bundles, which
	// are reviewed offline and benefit from a deeper window.
	bundleEventLimit = 30
	// historyEventLimit bounds the events recorded per history sample.
	historyEventLimit = 50
)
