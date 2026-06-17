package cmd

import (
	"context"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

func newPathCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "path NAME [NAME...]",
		Short:             "Explain the path from HPA desired replicas to ready pods",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPath(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runPath(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetPath)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}
