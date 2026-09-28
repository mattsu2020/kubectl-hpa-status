package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
)

func newTimelineCommand(opts *options) *cobra.Command {
	var duration time.Duration
	var interval time.Duration
	var since time.Duration
	var replay bool
	var fromRecord string

	cmd := &cobra.Command{
		Use:               "timeline NAME",
		Short:             "Show HPA scaling decisions over time (live or retrospective)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if fromRecord != "" {
				return runTimelineFromRecord(cmd.OutOrStdout(), opts, args[0], fromRecord)
			}
			// Retrospective mode takes priority when --since is provided.
			if since > 0 {
				return runRetrospectiveTimeline(cmd.Context(), cmd.OutOrStdout(), opts, args[0], since, replay)
			}
			// Existing live-polling behavior.
			if duration > 0 {
				var cancel context.CancelFunc
				ctx, cancel := context.WithTimeout(cmd.Context(), duration)
				defer cancel()
				return runTimeline(ctx, cmd.OutOrStdout(), opts, args[0], interval)
			}
			return runTimeline(cmd.Context(), cmd.OutOrStdout(), opts, args[0], interval)
		},
	}
	cmd.Flags().DurationVar(&duration, "duration", 10*time.Minute, "total observation duration")
	cmd.Flags().DurationVar(&interval, "interval", defaultPollInterval, "polling interval")
	cmd.Flags().DurationVar(&since, "since", 0, "show retrospective timeline for the given duration (e.g. 30m, 1h); 0 means live mode")
	cmd.Flags().BoolVar(&replay, "replay", false, "enhanced retrospective replay with bottleneck markers and control cycle analysis")
	cmd.Flags().StringVar(&fromRecord, "from-record", "", "read durable JSONL/JSON trace written by record instead of Kubernetes events")
	return cmd
}

func runRetrospectiveTimeline(ctx context.Context, out io.Writer, opts *options, name string, since time.Duration, replay bool) error {
	if err := validateTimelineOutput(opts, false); err != nil {
		return err
	}
	client, hpa, err := lookupHPA(ctx, opts, name)
	if err != nil {
		return err
	}

	// 2. Fetch events since the cutoff time.
	sinceTime := opts.CurrentTime().Add(-since)
	coreEvents, err := kube.FetchRecentHPAEventsForObjectSince(ctx, client.Interface, hpa, sinceTime)
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	events := hpaanalysis.EventsFromCore(coreEvents)

	// 3. Build the retrospective timeline.
	tl := retrospective.BuildTimeline(events, hpa, sinceTime)

	// 4. If replay mode is enabled, perform replay analysis.
	var replayAnalysis *retrospective.ReplayAnalysis
	if replay {
		replayAnalysis = retrospective.AnalyzeReplay(tl, hpa)
	}

	// 5. Render based on output format.
	format, _ := selectOutputFromOptions(opts)

	// Replay mode rendering.
	if replay && replayAnalysis != nil {
		return renderRetrospectiveReplay(out, replayAnalysis, tl, format, opts)
	}

	// Normal retrospective rendering.
	return renderRetrospective(out, tl, format, opts)
}

func renderRetrospectiveReplay(out io.Writer, replayAnalysis *retrospective.ReplayAnalysis, tl retrospective.Timeline, format string, opts *options) error {
	switch format {
	case "markdown", "md":
		return retrospective.WriteReplayMarkdown(out, replayAnalysis, tl)
	case "html":
		return retrospective.WriteReplayHTML(out, replayAnalysis, tl)
	default:
		_, templateStr := selectOutputFromOptions(opts)
		return render.Format(out, format, templateStr, replayAnalysis, func(out io.Writer) error {
			return retrospective.WriteReplayText(out, replayAnalysis, tl, themeFor(opts.Color, out))
		})
	}
}

func renderRetrospective(out io.Writer, tl retrospective.Timeline, format string, opts *options) error {
	switch format {
	case "markdown", "md":
		return retrospective.WriteMarkdown(out, tl)
	case "html":
		return retrospective.WriteHTML(out, tl)
	default:
		_, templateStr := selectOutputFromOptions(opts)
		return render.Format(out, format, templateStr, tl, func(out io.Writer) error {
			return retrospective.WriteTimeline(out, tl, themeFor(opts.Color, out))
		})
	}
}

func runTimeline(ctx context.Context, out io.Writer, opts *options, name string, interval time.Duration) error {
	if err := validateTimelineOutput(opts, true); err != nil {
		return err
	}
	if interval < time.Second {
		var clampErr error
		interval, clampErr = clampPollInterval(out, interval)
		if clampErr != nil {
			return clampErr
		}
	}

	theme := themeFor(opts.Color, out)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}
	ec := newEnrichmentContext(ctx, opts)
	var snapshots []hpaanalysis.TimelineSnapshot
	const maxTimelineSnapshots = 500

	for {
		report, err := buildStatusReport(ctx, opts, client, name, true, ec)
		if err != nil {
			return err
		}
		snapshot := hpaanalysis.SnapshotFromReport(report)
		snapshots = append(snapshots, snapshot)
		if len(snapshots) > maxTimelineSnapshots {
			copy(snapshots, snapshots[len(snapshots)-maxTimelineSnapshots:])
			snapshots = snapshots[:maxTimelineSnapshots]
		}

		if clearScreen := theme.ScreenClear(); clearScreen != "" {
			if _, err := out.Write([]byte(clearScreen)); err != nil {
				return err
			}
		}

		trace := hpaanalysis.TimelineTrace{
			HPAName:   name,
			Namespace: report.Analysis.Meta.Namespace,
			Start:     snapshots[0].Timestamp,
			Interval:  interval,
			Snapshots: snapshots,
		}
		if err := hpaanalysis.WriteTimelineTable(out, trace, theme); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runTimelineFromRecord(out io.Writer, opts *options, name, path string) error {
	if err := validateTimelineOutput(opts, false); err != nil {
		return err
	}
	trace, err := loadRecordedTrace(path, opts.Namespace, name)
	if err != nil {
		return err
	}
	format, templateStr := selectOutputFromOptions(opts)
	switch format {
	case "markdown", "md":
		return hpaanalysis.WriteTimelineMarkdown(out, *trace)
	case "html":
		return hpaanalysis.WriteTimelineHTML(out, *trace)
	}
	return render.Format(out, format, templateStr, trace, func(out io.Writer) error {
		theme := themeFor(opts.Color, out)
		return hpaanalysis.WriteTimelineTable(out, *trace, theme)
	})
}

func isKnownOutputFormat(format string) bool {
	switch format {
	case "", "table", "wide", "ja", "json", "yaml", "markdown", "md", "html", "incident", "prometheus":
		return true
	default:
		return strings.HasPrefix(format, "jsonpath") || strings.HasPrefix(format, "template") || strings.HasPrefix(format, "go-template")
	}
}

func runReplay(out io.Writer, opts *options, filePath string) error {
	data, err := readFileBounded(filePath)
	if err != nil {
		return fmt.Errorf("failed to read trace file: %w", err)
	}

	var trace hpaanalysis.TimelineTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return fmt.Errorf("failed to parse trace file: %w", err)
	}

	format, templateStr := selectOutputFromOptions(opts)
	switch format {
	case "markdown", "md":
		return hpaanalysis.WriteTimelineMarkdown(out, trace)
	case "html":
		return hpaanalysis.WriteTimelineHTML(out, trace)
	}
	return render.Format(out, format, templateStr, trace, func(out io.Writer) error {
		theme := themeFor(opts.Color, out)
		return hpaanalysis.WriteTimelineTable(out, trace, theme)
	})
}

// validateTimelineOutput rejects unsupported modes before Kubernetes or file I/O.
// Live output redraws a table; use record for a durable structured stream.
func validateTimelineOutput(opts *options, live bool) error {
	format, _ := selectOutputFromOptions(opts)
	switch format {
	case "", "table", "wide", "ja":
		return nil
	}
	if live {
		return fmt.Errorf("live timeline does not support --output=%s; use --since or --from-record for structured reports, or record for live recording", format)
	}
	switch format {
	case "json", "jsonl", "yaml", "jsonpath", "go-template", "template", "markdown", "md", "html":
		return nil
	}
	if _, _, ok := render.ParsePrefixedFormat(format); ok {
		return nil
	}
	return fmt.Errorf("timeline does not support --output=%s", format)
}
