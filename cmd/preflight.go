package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newPreflightCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "preflight NAME [NAME...]",
		Short:             "Validate quota and capacity before raising HPA maxReplicas",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			raiseMax, _ := cmd.Flags().GetInt32("raise-max")
			return runPreflight(cmd.Context(), cmd.OutOrStdout(), opts, args, raiseMax)
		},
	}
	cmd.Flags().Int32("raise-max", 0, "proposed maxReplicas value to validate")
	return cmd
}

func runPreflight(ctx context.Context, out io.Writer, opts *options, names []string, raiseMax int32) error {
	local := copyOptions(opts)
	local.targetMax = raiseMax
	local.checkResources = true
	local.capacityContext = true
	local.capacityDeep = true
	local.explainPods = true
	return runCapacityPlan(ctx, out, &local, names)
}
