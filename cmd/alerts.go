package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/alerts"
)

// The alert-rule templates live in cmd/internal/alerts; this file keeps only
// the cobra wiring.
func newAlertsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Generate alert rules from kubectl-hpa-status health semantics",
		Args:  cobra.NoArgs,
	}
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate Prometheus or Datadog alert rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			rules, err := alerts.Rules(format)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), rules)
			return err
		},
	}
	generate.Flags().String("format", string(alerts.FormatPrometheus), "alert rule format: prometheus or datadog")
	cmd.AddCommand(generate)
	return cmd
}
