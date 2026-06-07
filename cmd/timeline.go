package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func newTimelineCommand(opts *options) *cobra.Command {
	var duration time.Duration
	var interval time.Duration
	var since time.Duration
	var replay bool

	cmd := &cobra.Command{
		Use:               "timeline NAME",
		Short:             "Show HPA scaling decisions over time (live or retrospective)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
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
	return cmd
}

func newRecordCommand(opts *options) *cobra.Command {
	var duration time.Duration
	var interval time.Duration

	cmd := &cobra.Command{
		Use:               "record NAME",
		Short:             "Record HPA timeline snapshots to a JSON file",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputPath := opts.output
			if outputPath == "" {
				outputPath = "hpa-trace.json"
			}
			if duration > 0 {
				var cancel context.CancelFunc
				ctx, cancel := context.WithTimeout(cmd.Context(), duration)
				defer cancel()
				return runRecord(ctx, cmd.OutOrStdout(), opts, args[0], interval, outputPath)
			}
			return runRecord(cmd.Context(), cmd.OutOrStdout(), opts, args[0], interval, outputPath)
		},
	}
	cmd.Flags().DurationVar(&duration, "duration", 15*time.Minute, "total recording duration")
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Second, "polling interval")
	return cmd
}

func newReplayCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay FILE",
		Short: "Replay a recorded HPA timeline trace from a JSON file",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplay(cmd.OutOrStdout(), opts, args[0])
		},
	}
	return cmd
}

func runRetrospectiveTimeline(ctx context.Context, out io.Writer, opts *options, name string, since time.Duration, replay bool) error {
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// 1. Fetch the HPA object (needed for behavior, conditions, metrics).
	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", client.Namespace, name, err)
	}

	// 2. Fetch events since the cutoff time.
	sinceTime := time.Now().Add(-since)
	events, err := hpaanalysis.RecentEventsSince(ctx, client.Interface, hpa.Namespace, hpa.Name, sinceTime)
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}

	// 3. Build the retrospective timeline.
	tl := hpaanalysis.BuildRetrospectiveTimeline(events, hpa, sinceTime)

	// 4. If replay mode is enabled, perform replay analysis.
	var replayAnalysis *hpaanalysis.ReplayAnalysis
	if replay {
		replayAnalysis = hpaanalysis.AnalyzeReplay(tl, hpa)
	}

	// 5. Render based on output format.
	format, _ := outputSelection(outputConfig{
		report: opts.report, output: opts.output, template: opts.template,
		outputTemplates: opts.outputTemplates,
	})

	// Replay mode rendering.
	if replay && replayAnalysis != nil {
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
			_, err = out.Write(data)
			return err
		case "markdown", "md":
			return hpaanalysis.WriteReplayMarkdown(out, replayAnalysis, tl)
		case "html":
			return hpaanalysis.WriteReplayHTML(out, replayAnalysis, tl)
		default:
			theme := style.NewTheme(shouldColorize(opts.color, out))
			return hpaanalysis.WriteReplayText(out, replayAnalysis, tl, theme)
		}
	}

	// Normal retrospective rendering.
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
		_, err = out.Write(data)
		return err
	case "markdown", "md":
		return hpaanalysis.WriteRetrospectiveMarkdown(out, tl)
	case "html":
		return hpaanalysis.WriteRetrospectiveHTML(out, tl)
	default:
		theme := style.NewTheme(shouldColorize(opts.color, out))
		return hpaanalysis.WriteRetrospectiveTimeline(out, tl, theme)
	}
}

func runTimeline(ctx context.Context, out io.Writer, opts *options, name string, interval time.Duration) error {
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ec := newEnrichmentContext(ctx, opts)
	var snapshots []hpaanalysis.TimelineSnapshot

	for {
		report, err := buildStatusReportWithClient(ctx, opts, name, true, ec)
		if err != nil {
			return err
		}
		snapshot := hpaanalysis.SnapshotFromReport(report)
		snapshots = append(snapshots, snapshot)

		if clearScreen := theme.ScreenClear(); clearScreen != "" {
			if _, err := out.Write([]byte(clearScreen)); err != nil {
				return err
			}
		}

		trace := hpaanalysis.TimelineTrace{
			HPAName:   name,
			Namespace: opts.namespace,
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

func runRecord(ctx context.Context, out io.Writer, opts *options, name string, interval time.Duration, outputPath string) error {
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ec := newEnrichmentContext(ctx, opts)
	var snapshots []hpaanalysis.TimelineSnapshot
	start := time.Now()

	for {
		report, err := buildStatusReportWithClient(ctx, opts, name, true, ec)
		if err != nil {
			return err
		}
		snapshot := hpaanalysis.SnapshotFromReport(report)
		snapshots = append(snapshots, snapshot)
		_, _ = fmt.Fprintf(out, "Recorded snapshot #%d at %s (current=%d desired=%d health=%s)\n",
			len(snapshots), snapshot.Timestamp.Format(time.RFC3339), snapshot.Current, snapshot.Desired, snapshot.Health)

		select {
		case <-ctx.Done():
			trace := hpaanalysis.TimelineTrace{
				HPAName:   name,
				Namespace: opts.namespace,
				Start:     start,
				End:       time.Now(),
				Interval:  interval,
				Snapshots: snapshots,
			}
			return writeTrace(outputPath, trace, out)
		case <-ticker.C:
		}
	}
}

func runReplay(out io.Writer, opts *options, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read trace file: %w", err)
	}

	var trace hpaanalysis.TimelineTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return fmt.Errorf("failed to parse trace file: %w", err)
	}

	format, _ := outputSelection(outputConfig{report: opts.report, output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates})
	switch format {
	case "markdown", "md":
		return hpaanalysis.WriteTimelineMarkdown(out, trace)
	case "html":
		return hpaanalysis.WriteTimelineHTML(out, trace)
	default:
		theme := style.NewTheme(shouldColorize(opts.color, out))
		return hpaanalysis.WriteTimelineTable(out, trace, theme)
	}
}

// writeTrace writes the TimelineTrace to a JSON file.
func writeTrace(path string, trace hpaanalysis.TimelineTrace, out io.Writer) error {
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal trace: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write trace file: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Trace saved to %s (%d snapshots)\n", path, len(trace.Snapshots))
	return nil
}
