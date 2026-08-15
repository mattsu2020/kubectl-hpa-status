package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

// newDoctorCommand builds the doctor workflow parent. Bare `doctor NAME` keeps
// the full preset diagnosis; the focused diagnosis presets live as subcommands
// (`doctor readiness|rollout|capacity|trace|path|preflight`) so the top-level
// --help stays small. The historical top-level command names remain available
// as hidden deprecated aliases for the v3 line (see deprecated_aliases.go).
func newDoctorCommand(opts *options) *cobra.Command {
	doctor := &cobra.Command{
		Use:               "doctor NAME [NAME...]",
		Short:             "Diagnose HPA scaling failures across metrics, workload, pods, resources, events, and KEDA",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
	doctor.AddCommand(doctorPresetSubcommands(opts)...)
	return doctor
}

// doctorPresetSubcommands adapts the focused diagnosis preset commands into
// doctor subcommands. The constructors are shared with the deprecated
// top-level aliases, so the command bodies stay single-sourced; only the
// command name (the first word of Use) changes for the grouped paths whose
// historical name carries a "-context" suffix.
func doctorPresetSubcommands(opts *options) []*cobra.Command {
	rollout := newRolloutContextCommand(opts)
	rollout.Use = "rollout NAME [NAME...]"
	capacity := newNodeContextCommand(opts)
	capacity.Use = "capacity NAME [NAME...]"
	return []*cobra.Command{
		newReadinessCommand(opts),
		rollout,
		capacity,
		newTraceCommand(opts),
		newPathCommand(opts),
		newPreflightCommand(opts),
	}
}

func runDoctor(ctx context.Context, out io.Writer, opts *options, names []string) error {
	return runStatusWithPreset(ctx, out, opts, presetDoctor, names)
}
