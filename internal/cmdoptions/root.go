// Package cmdoptions defines the structured CLI option model shared across all
// kubectl-hpa-status commands. Root composes the per-workflow option groups
// (Common, Status, List, Watch); commands read and mutate copies via presets
// and normalization helpers rather than reaching into cobra flag bindings.
package cmdoptions

import "time"

// Root composes all CLI option groups. Commands access fields through
// embedded struct promotion (e.g. opts.Namespace, opts.Explain).
type Root struct {
	Common
	Status
	List
	Watch
}

// DefaultRoot returns a Root with documented CLI defaults.
func DefaultRoot() Root {
	return Root{
		Common: Common{
			Color:     "auto",
			ChunkSize: 500,
			DryRun:    true,
		},
		Status: Status{
			Events: EventOption{Enabled: true, Limit: 5},
		},
		List: List{
			HealthScoreMax: -1,
		},
		Watch: Watch{
			WatchInterval: 5 * time.Second,
		},
	}
}

// Copy returns a shallow copy suitable for per-command mutation.
// Reference-typed fields (ClientOverride, OutputTemplates, slices) are
// shared by value; deep-copy them explicitly when divergence is required.
func (r Root) Copy() Root {
	return r
}
