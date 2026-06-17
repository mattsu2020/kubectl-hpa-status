package cmd

import (
	"context"
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

func newContainerAdvisorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "container-advisor NAME [NAME...]",
		Short:             "Suggest ContainerResource metrics for multi-container HPA targets",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContainerAdvisor(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runContainerAdvisor(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := applyCommandPreset(opts, cmdoptions.PresetContainerAdvisor)
	return runStatusMany(ctx, out, &local, names, !local.NoInterpret)
}