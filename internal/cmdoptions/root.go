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

// Copy returns a copy suitable for per-command mutation.
//
// Data fields that callers commonly mutate per command — slices
// (HealthWeightOverrides, Simulate, SimulateMetric), maps
// (OutputTemplates), and the HealthWeights struct (which holds *int
// pointers) — are deep-copied so mutating the returned Root never leaks
// back into the original.
//
// The two input-port fields, ClientOverride (a kubernetes.Interface) and
// In (an io.Reader), are intentionally shared: they describe a live
// client/stdin the copy should keep using, not data to fork. If a caller
// needs to swap the client or input, set the field explicitly after Copy.
func (r Root) Copy() Root {
	clone := r // value copy: all scalar/struct fields are now independent

	// Deep-copy slices so append/reassignment on the copy does not resize
	// or alias the original's backing array.
	clone.HealthWeightOverrides = cloneStrings(r.HealthWeightOverrides)
	clone.Simulate = cloneStrings(r.Simulate)
	clone.SimulateMetric = cloneStrings(r.SimulateMetric)

	// Deep-copy the output-templates map.
	if r.OutputTemplates != nil {
		clone.OutputTemplates = make(map[string]OutputTemplateConfig, len(r.OutputTemplates))
		for k, v := range r.OutputTemplates {
			clone.OutputTemplates[k] = v
		}
	}

	// Deep-copy HealthWeights so flipping a *int penalty on the copy does
	// not mutate the shared original.
	clone.HealthWeights = r.HealthWeights.Clone()

	// ClientOverride and In are intentionally shared (see doc comment).
	return clone
}

// cloneStrings returns a fresh copy of s (or nil when s is nil/empty) so the
// caller can append or reassign without aliasing the source backing array.
func cloneStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}
