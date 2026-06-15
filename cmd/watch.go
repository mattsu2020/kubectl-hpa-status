package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
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
	if opts.dashboard && opts.output == "" && isInteractiveTerminal(out) {
		return runTUI(ctx, out, opts, name, true)
	}

	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	interval := opts.watchInterval
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ec := newEnrichmentContext(ctx, opts)

	var previous *hpaanalysis.Analysis
	for {
		if err := clearWatchScreen(out, theme); err != nil {
			return err
		}

		report, err := buildStatusReportWithClient(ctx, opts, name, includeInterpretation, ec)
		if err != nil {
			return err
		}
		if err := writeWatchReport(out, opts, theme, report, previous); err != nil {
			return err
		}
		previous = &report.Analysis

		writeStabilizationCountdown(out, &report.Analysis)

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

// clearWatchScreen clears the terminal via the theme's screen-clear sequence, or prints a timestamp header when unavailable.
func clearWatchScreen(out io.Writer, theme style.Theme) error {
	if clearScreen := theme.ScreenClear(); clearScreen != "" {
		if _, err := out.Write([]byte(clearScreen)); err != nil {
			return err
		}
		return nil
	}
	_, err := fmt.Fprintf(out, "Updated: %s\n\n", time.Now().Format(time.RFC3339))
	return err
}

// writeWatchReport renders the current report via the selected format, choosing dashboard/diff/text rendering inside the fallback.
func writeWatchReport(out io.Writer, opts *options, theme style.Theme, report hpaanalysis.StatusReport, previous *hpaanalysis.Analysis) error {
	format, templateStr := outputSelection(outputConfig{report: opts.report, output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates})
	return writeOutput(out, format, templateStr, report, func() error {
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
	})
}

// writeStabilizationCountdown prints the prominent stabilization countdown line when scale-down stabilization is active.
func writeStabilizationCountdown(out io.Writer, a *hpaanalysis.Analysis) {
	if a.StabilizationRemaining == nil || *a.StabilizationRemaining <= 0 {
		return
	}
	source := a.StabilizationSource
	if source == "" {
		source = "scaleDown"
	}
	progress := hpaanalysis.FormatStabilizationProgress(
		a.StabilizationRemaining,
		a.StabilizationWindowSeconds,
	)
	_, _ = fmt.Fprintf(out, "\n  STABILIZING: %s [%s] [estimated]\n", progress, source)
}

func runWatchList(ctx context.Context, out io.Writer, opts *options) error {
	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	interval := opts.watchInterval
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if clearScreen := theme.ScreenClear(); clearScreen != "" {
			if _, err := out.Write([]byte(clearScreen)); err != nil {
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
