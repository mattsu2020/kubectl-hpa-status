package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newContainerAdvisorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "container-advisor NAME [NAME...]",
		Aliases:           []string{"container-metric"},
		Short:             "Suggest ContainerResource HPA metrics for multi-container workloads",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContainerAdvisor(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runContainerAdvisor(ctx context.Context, out io.Writer, opts *options, names []string) error {
	local := copyOptions(opts)
	local.explain = true
	local.explainPods = true
	local.checkResources = true
	local.containerAdvisor = true
	return runStatusMany(ctx, out, &local, names, !local.noInterpret)
}
