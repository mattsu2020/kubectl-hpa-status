package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

func newWatchCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "watch NAME",
		Short:             "Watch one HPA status",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.NoInterpret)
		},
	}
	return cmd
}

func runWatch(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	if opts.Dashboard && opts.Output == "" && isInteractiveTerminal(out) {
		return runTUI(ctx, out, opts, name, true)
	}

	ctx, cancel, ticker := startWatchLoop(ctx, out, opts)
	defer cancel()
	defer ticker.Stop()

	theme := themeFor(opts.Color, out)
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	ec := newEnrichmentContext(ctx, opts)
	return runWatchPolling(ctx, out, opts, client, ec, name, includeInterpretation, theme, ticker)
}

func runWatchPolling(ctx context.Context, out io.Writer, opts *options, client *kube.Client, ec *enrichmentContext, name string, includeInterpretation bool, theme style.Theme, ticker *time.Ticker) error {
	var previous *hpaanalysis.Analysis
	humanOutput := watchUsesHumanOutput(opts)
	for {
		if humanOutput {
			if err := clearWatchScreen(out, theme, opts.CurrentTime()); err != nil {
				return err
			}
		} else if err := writeWatchDocumentSeparator(out, opts); err != nil {
			return err
		}

		report, err := buildStatusReport(ctx, opts, client, name, includeInterpretation, ec)
		if err != nil {
			return err
		}
		if err := writeWatchReport(out, opts, report, previous); err != nil {
			return err
		}
		previous = &report.Analysis

		if humanOutput {
			writeStabilizationCountdown(out, &report.Analysis)
		}

		if opts.UntilCondition != "" && reportHasCondition(report, opts.UntilCondition) {
			_, err := fmt.Fprintf(watchDiagnosticWriter(opts, out), "\nStopped: condition %q is present.\n", opts.UntilCondition)
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if humanOutput {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
		}
	}
}

func watchUsesHumanOutput(opts *options) bool {
	format, _ := selectOutputFromOptions(opts)
	switch normalizeOutputFormat(format) {
	case "", "table", "wide", "ja":
		return true
	default:
		return false
	}
}

func watchDiagnosticWriter(opts *options, fallback io.Writer) io.Writer {
	if watchUsesHumanOutput(opts) {
		return errorWriter(opts, fallback)
	}
	return io.Discard
}

func writeWatchDocumentSeparator(out io.Writer, opts *options) error {
	format, _ := selectOutputFromOptions(opts)
	if normalizeOutputFormat(format) == "yaml" {
		if _, err := fmt.Fprintln(out, "---"); err != nil {
			return err
		}
	}
	return nil
}

// clearWatchScreen clears the terminal via the theme's screen-clear sequence, or prints a timestamp header when unavailable.
func clearWatchScreen(out io.Writer, theme style.Theme, now time.Time) error {
	if clearScreen := theme.ScreenClear(); clearScreen != "" {
		if _, err := out.Write([]byte(clearScreen)); err != nil {
			return err
		}
		return nil
	}
	_, err := fmt.Fprintf(out, "Updated: %s\n\n", now.Format(time.RFC3339))
	return err
}

// writeWatchReport renders the current report via the selected format, choosing dashboard/diff/text rendering inside the fallback.
// All three paths thread StatusTextOptions so the Summary line is localised when --lang is set.
func writeWatchReport(out io.Writer, opts *options, report hpaanalysis.StatusReport, previous *hpaanalysis.Analysis) error {
	return renderWithOutput(out, opts, statusOutputValue(opts, report), func(out io.Writer) error {
		textOpts := statusTextOptions(opts, out)
		if opts.Dashboard {
			return hpaanalysis.WriteStatusDashboardWithOptions(out, report, textOpts)
		}
		if previous != nil {
			return hpaanalysis.WriteStatusDiffWithOptions(out, hpaanalysis.WatchState{
				Previous: previous,
				Current:  &report.Analysis,
			}, textOpts)
		}
		return hpaanalysis.WriteStatusTextWithOptions(out, report, textOpts)
	})

}

// writeStabilizationCountdown prints the prominent stabilization countdown line when scale-down stabilization is active.
func writeStabilizationCountdown(out io.Writer, a *hpaanalysis.Analysis) {
	if a.StabilizationRemaining() == nil || *a.StabilizationRemaining() <= 0 {
		return
	}
	source := a.StabilizationSource()
	if source == "" {
		source = "scaleDown"
	}
	progress := hpaanalysis.FormatStabilizationProgress(
		a.StabilizationRemaining(),
		a.StabilizationWindowSeconds(),
	)
	_, _ = fmt.Fprintf(out, "\n  STABILIZING: %s [%s] [estimated]\n", progress, source)
}

// minWatchInterval protects the API server from polling floods.
const minWatchInterval = time.Second

// startWatchLoop applies the shared watch prologue: the optional timeout
// context, the interval clamp warning, and the ticker. Callers must defer both
// returned release functions.
func startWatchLoop(ctx context.Context, out io.Writer, opts *options) (context.Context, context.CancelFunc, *time.Ticker) {
	cancel := context.CancelFunc(func() {})
	if opts.WatchTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.WatchTimeout)
	}

	interval := opts.WatchInterval
	if interval < minWatchInterval {
		_, _ = fmt.Fprintf(watchDiagnosticWriter(opts, out), "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = minWatchInterval
	}
	return ctx, cancel, time.NewTicker(interval)
}

// waitWatchTick blocks until the next tick or cancellation, printing the
// human-output blank separator line between frames.
func waitWatchTick(ctx context.Context, out io.Writer, ticker *time.Ticker, humanOutput bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
		if humanOutput {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		return nil
	}
}

func runWatchList(ctx context.Context, out io.Writer, opts *options) error {
	ctx, cancel, ticker := startWatchLoop(ctx, out, opts)
	defer cancel()
	defer ticker.Stop()

	theme := themeFor(opts.Color, out)
	humanOutput := watchUsesHumanOutput(opts)
	for {
		if humanOutput {
			if err := clearWatchScreen(out, theme, opts.CurrentTime()); err != nil {
				return err
			}
		} else if err := writeWatchDocumentSeparator(out, opts); err != nil {
			return err
		}

		if err := runList(ctx, out, opts); err != nil {
			return err
		}

		if err := waitWatchTick(ctx, out, ticker, humanOutput); err != nil {
			return err
		}
	}
}
