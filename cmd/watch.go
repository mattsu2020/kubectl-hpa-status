package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

func newWatchCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "watch NAME",
		Short:             "Watch one HPA status",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}
	return cmd
}

func runWatch(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	interval := opts.watchInterval
	if interval < time.Second {
		fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var previous *hpaanalysis.Analysis
	for {
		if clear := theme.ScreenClear(); clear != "" {
			if _, err := out.Write([]byte(clear)); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(out, "Updated: %s\n\n", time.Now().Format(time.RFC3339)); err != nil {
				return err
			}
		}

		report, err := buildStatusReport(ctx, opts, name, includeInterpretation)
		if err != nil {
			return err
		}
		if err := writeOutput(out, opts.output, opts.template, report, func() error {
			if opts.dashboard {
				return hpaanalysis.WriteStatusDashboard(out, report, theme)
			}
			if previous != nil {
				return hpaanalysis.WriteStatusDiff(out, hpaanalysis.WatchState{
					Previous: previous,
					Current:  &report.Analysis,
				}, theme)
			}
			return hpaanalysis.WriteStatusText(out, report, theme)
		}); err != nil {
			return err
		}
		previous = &report.Analysis

		if opts.untilCondition != "" && reportHasCondition(report, opts.untilCondition) {
			_, err := fmt.Fprintf(out, "\nStopped: condition %q is present.\n", opts.untilCondition)
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
}

func runWatchList(ctx context.Context, out io.Writer, opts *options) error {
	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	interval := opts.watchInterval
	if interval < time.Second {
		fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if clear := theme.ScreenClear(); clear != "" {
			if _, err := out.Write([]byte(clear)); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(out, "Updated: %s\n\n", time.Now().Format(time.RFC3339)); err != nil {
				return err
			}
		}

		if err := runList(ctx, out, opts); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
}
