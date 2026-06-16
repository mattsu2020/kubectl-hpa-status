package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newProfileCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Show HPA controller-manager timing profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProfile(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "detect",
		Short: "Detect or assume HPA controller-manager timing parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProfileDetect(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	})
	return cmd
}

func runProfile(ctx context.Context, out io.Writer, opts *options) error {
	client, err := opts.newClient()
	if err != nil {
		return err
	}
	profile := buildControllerProfile(ctx, client, opts.assumeProfile, opts.controllerProfileFile)
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

func runProfileDetect(ctx context.Context, out io.Writer, opts *options) error {
	client, err := opts.newClient()
	if err != nil {
		return err
	}
	profile := buildControllerProfile(ctx, client, opts.assumeProfile, opts.controllerProfileFile)
	return writeOutput(out, opts.output, opts.template, profile, func() error {
		confidence := "medium"
		if profile.Source == "defaults" || len(profile.Warnings) > 0 {
			confidence = "low"
		}
		_, err := fmt.Fprintf(out, "HPA Controller Profile\n\nDetected:\n  source: %s\n\nAssumed / Effective:\n  default tolerance: %s\n  downscale stabilization: %s\n  cpu initialization period: %s\n  initial readiness delay: %s\n  sync period: %s\n\nConfidence:\n  controller flags: %s\n",
			profile.Source,
			profile.Tolerance,
			profile.DownscaleStabilization,
			profile.CPUInitializationPeriod,
			profile.InitialReadinessDelay,
			profile.SyncPeriod,
			confidence,
		)
		if err != nil {
			return err
		}
		if len(profile.Warnings) > 0 {
			_, _ = fmt.Fprintln(out, "  reason:")
			for _, warning := range profile.Warnings {
				if _, err := fmt.Fprintf(out, "    - %s\n", warning); err != nil {
					return err
				}
			}
		}
		_, err = fmt.Fprintln(out, "\nNext:\n  pass --controller-profile-file profile.yaml for more accurate analysis when controller-manager flags are hidden")
		return err
	})
}
