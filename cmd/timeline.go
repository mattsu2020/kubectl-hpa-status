package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
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
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Second, "polling interval")
	cmd.Flags().DurationVar(&since, "since", 0, "show retrospective timeline for the given duration (e.g. 30m, 1h); 0 means live mode")
	cmd.Flags().BoolVar(&replay, "replay", false, "enhanced retrospective replay with bottleneck markers and control cycle analysis")
	cmd.Flags().StringVar(&fromRecord, "from-record", "", "read durable JSONL/JSON trace written by record instead of Kubernetes events")
	return cmd
}

func runRetrospectiveTimeline(ctx context.Context, out io.Writer, opts *options, name string, since time.Duration, replay bool) error {
	client, hpa, err := lookupHPA(ctx, opts, name)
	if err != nil {
		return err
	}

	// 2. Fetch events since the cutoff time.
	sinceTime := time.Now().Add(-since)
	coreEvents, err := kube.FetchRecentHPAEventsSince(ctx, client.Interface, hpa.Namespace, hpa.Name, sinceTime)
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
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(replayAnalysis)
	case "yaml":
		data, marshalErr := yaml.Marshal(replayAnalysis)
		if marshalErr != nil {
			return marshalErr
		}
		_, err := out.Write(data)
		return err
	case "markdown", "md":
		return retrospective.WriteReplayMarkdown(out, replayAnalysis, tl)
	case "html":
		return retrospective.WriteReplayHTML(out, replayAnalysis, tl)
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return retrospective.WriteReplayText(out, replayAnalysis, tl, theme)
	}
}

func renderRetrospective(out io.Writer, tl retrospective.Timeline, format string, opts *options) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(tl)
	case "yaml":
		data, marshalErr := yaml.Marshal(tl)
		if marshalErr != nil {
			return marshalErr
		}
		_, err := out.Write(data)
		return err
	case "markdown", "md":
		return retrospective.WriteMarkdown(out, tl)
	case "html":
		return retrospective.WriteHTML(out, tl)
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return retrospective.WriteTimeline(out, tl, theme)
	}
}

func runTimeline(ctx context.Context, out io.Writer, opts *options, name string, interval time.Duration) error {
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	theme := style.NewTheme(shouldColorize(opts.Color, out))
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
			Namespace: opts.Namespace,
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
	trace, err := loadRecordedTrace(path, opts.Namespace, name)
	if err != nil {
		return err
	}
	format, _ := selectOutputFromOptions(opts)
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(trace)
	case "yaml":
		data, marshalErr := yaml.Marshal(trace)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = out.Write(data)
		return err
	case "markdown", "md":
		return hpaanalysis.WriteTimelineMarkdown(out, *trace)
	case "html":
		return hpaanalysis.WriteTimelineHTML(out, *trace)
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return hpaanalysis.WriteTimelineTable(out, *trace, theme)
	}
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

	format, _ := selectOutputFromOptions(opts)
	switch format {
	case "markdown", "md":
		return hpaanalysis.WriteTimelineMarkdown(out, trace)
	case "html":
		return hpaanalysis.WriteTimelineHTML(out, trace)
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return hpaanalysis.WriteTimelineTable(out, trace, theme)
	}
}
