package cmd

import (
	"github.com/spf13/cobra"
)

func newExportCommand(opts *options) *cobra.Command {
	var prometheus bool
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export HPA health data for external systems",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !prometheus {
				return cmd.Help()
			}
			local := *opts
			local.output = "prometheus"
			return runList(cmd.Context(), cmd.OutOrStdout(), &local)
		},
	}
	cmd.Flags().BoolVar(&prometheus, "prometheus", false, "export HPA health metrics in Prometheus exposition format")
	return cmd
}
