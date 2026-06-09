package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newProfileCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "profile",
		Short: "Show HPA controller-manager timing profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProfile(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
}

func runProfile(ctx context.Context, out io.Writer, opts *options) error {
	client, err := opts.newClient()
	if err != nil {
		return err
	}
	profile := buildControllerProfile(ctx, client, opts)
	return writeOutput(out, opts.output, opts.template, profile, func() error {
		_, err := fmt.Fprintf(out, "Controller profile:\n  source: %s\n  sync period: %s\n  downscale stabilization: %s\n  initial readiness delay: %s\n  cpu initialization period: %s\n  tolerance: %s\n",
			profile.Source,
			profile.SyncPeriod,
			profile.DownscaleStabilization,
			profile.InitialReadinessDelay,
			profile.CPUInitializationPeriod,
			profile.Tolerance,
		)
		if err != nil {
			return err
		}
		for _, warning := range profile.Warnings {
			if _, err := fmt.Fprintf(out, "  warning: %s\n", warning); err != nil {
				return err
			}
		}
		return nil
	})
}
