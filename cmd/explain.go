package cmd

import (
	"github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

func newExplainCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "explain NAME [NAME...]",
		Short:             "Export structured HPA scaling decision evidence",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			local := applyCommandPreset(opts, cmdoptions.PresetExplain, cmdoptions.CommandPresetOptions{
				StructuredFormat: true,
			})
			return runStatusMany(cmd.Context(), cmd.OutOrStdout(), &local, args, true)
		},
	}
	return cmd
}