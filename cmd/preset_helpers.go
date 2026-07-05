package cmd

import (
	"context"
	"io"
)

// runStatusWithPreset applies a command preset to a copy of opts and delegates
// to runStatusMany with the standard "interpret unless --no-interpret" flag.
// It collapses the repeated two-line pattern:
//
//	local := applyCommandPreset(opts, presetX)
//	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
//
// Callers that need to mutate the preset copy before running (e.g. doctor's
// extra event/trend flags, history's --since wiring) must keep using
// applyCommandPreset directly; this helper is only for the no-mutation case.
func runStatusWithPreset(ctx context.Context, out io.Writer, opts *options, p preset, names []string, extra ...commandPresetOptions) error {
	local := applyCommandPreset(opts, p, extra...)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}
