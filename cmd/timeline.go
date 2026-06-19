package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
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
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	// 1. Fetch the HPA object (needed for behavior, conditions, metrics).
	hpa, err := kube.GetHPAFromClient(ctx, client, name)
	if err != nil {
		return fmt.Errorf("failed to get HPA %s/%s: %w", client.Namespace, name, err)
	}

	// 2. Fetch events since the cutoff time.
	sinceTime := time.Now().Add(-since)
	coreEvents, err := kube.FetchRecentHPAEventsSince(ctx, client.Interface, hpa.Namespace, hpa.Name, sinceTime)
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	events := hpaanalysis.EventsFromCore(coreEvents)

	// 3. Build the retrospective timeline.
	tl := hpaanalysis.BuildRetrospectiveTimeline(events, hpa, sinceTime)

	// 4. If replay mode is enabled, perform replay analysis.
	var replayAnalysis *hpaanalysis.ReplayAnalysis
	if replay {
		replayAnalysis = hpaanalysis.AnalyzeReplay(tl, hpa)
	}

	// 5. Render based on output format.
	format, _ := outputSelection(outputConfig{
		report: opts.Report, output: opts.Output, template: opts.Template,
		outputTemplates: opts.OutputTemplates,
	})

	// Replay mode rendering.
	if replay && replayAnalysis != nil {
		return renderRetrospectiveReplay(out, replayAnalysis, tl, format, opts)
	}

	// Normal retrospective rendering.
	return renderRetrospective(out, tl, format, opts)
}

func renderRetrospectiveReplay(out io.Writer, replayAnalysis *hpaanalysis.ReplayAnalysis, tl hpaanalysis.RetrospectiveTimeline, format string, opts *options) error {
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
		return hpaanalysis.WriteReplayMarkdown(out, replayAnalysis, tl)
	case "html":
		return hpaanalysis.WriteReplayHTML(out, replayAnalysis, tl)
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return hpaanalysis.WriteReplayText(out, replayAnalysis, tl, theme)
	}
}

func renderRetrospective(out io.Writer, tl hpaanalysis.RetrospectiveTimeline, format string, opts *options) error {
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
		return hpaanalysis.WriteRetrospectiveMarkdown(out, tl)
	case "html":
		return hpaanalysis.WriteRetrospectiveHTML(out, tl)
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		return hpaanalysis.WriteRetrospectiveTimeline(out, tl, theme)
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

func runRecord(ctx context.Context, out io.Writer, opts *options, name string, interval time.Duration, outputPath string) error {
	if interval < time.Second {
		_, _ = fmt.Fprintf(out, "Warning: interval %s is below 1s; clamping to 1s to reduce API server load.\n", interval)
		interval = time.Second
	}

	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open record file: %w", err)
	}
	defer func() { _ = file.Close() }()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ec := newEnrichmentContext(ctx, opts)
	start := time.Now()
	counts := map[string]int{}
	previous := map[string]hpaanalysis.TimelineSnapshot{}
	interestingChanges := map[string][]string{}

	for {
		records, err := recordOnce(ctx, opts, name, interval, ec)
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := writeRecordLine(file, record); err != nil {
				return err
			}
			key := record.Namespace + "/" + record.HPAName
			counts[key]++
			if len(record.Snapshots) > 0 {
				snapshot := record.Snapshots[0]
				if prev, ok := previous[key]; ok {
					for _, change := range hpaanalysis.DiffSnapshots(prev, snapshot) {
						interestingChanges[key] = append(interestingChanges[key],
							fmt.Sprintf("%s %s", snapshot.Timestamp.Format("15:04"), change))
					}
				}
				previous[key] = snapshot
			}
		}
		_, _ = fmt.Fprintf(out, "Recorded %d snapshot(s) at %s\n", len(records), time.Now().Format(time.RFC3339))

		select {
		case <-ctx.Done():
			return writeRecordSummary(out, outputPath, counts, interestingChanges, start)
		case <-ticker.C:
		}
	}
}

func recordOnce(ctx context.Context, opts *options, name string, interval time.Duration, ec *enrichmentContext) ([]hpaanalysis.TimelineTrace, error) {
	if name != "" {
		report, err := buildStatusReportWithClient(ctx, opts, name, true, ec)
		if err != nil {
			return nil, err
		}
		return []hpaanalysis.TimelineTrace{traceFromReport(report, interval)}, nil
	}

	client, err := newClientOrDefault(opts)
	if err != nil {
		return nil, err
	}
	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = metav1.NamespaceAll
	}
	var records []hpaanalysis.TimelineTrace
	err = kube.ListHPAsEachPage(ctx, client.Interface, namespace, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize, func(page *autoscalingv2.HorizontalPodAutoscalerList) error {
		for i := range page.Items {
			local := copyOptions(opts)
			local.Namespace = page.Items[i].Namespace
			report, err := buildStatusReportWithClient(ctx, &local, page.Items[i].Name, true, ec)
			if err != nil {
				return err
			}
			records = append(records, traceFromReport(report, interval))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list HPAs: %w", err)
	}
	return records, nil
}

func traceFromReport(report hpaanalysis.StatusReport, interval time.Duration) hpaanalysis.TimelineTrace {
	snapshot := hpaanalysis.SnapshotFromReport(report)
	return hpaanalysis.TimelineTrace{
		HPAName:   report.Analysis.Name,
		Namespace: report.Analysis.Namespace,
		Start:     snapshot.Timestamp,
		End:       snapshot.Timestamp,
		Interval:  interval,
		Snapshots: []hpaanalysis.TimelineSnapshot{snapshot},
	}
}

func writeRecordLine(w io.Writer, trace hpaanalysis.TimelineTrace) error {
	data, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write record line: %w", err)
	}
	return nil
}

func writeRecordSummary(out io.Writer, path string, counts map[string]int, changes map[string][]string, start time.Time) error {
	total := 0
	for _, count := range counts {
		total += count
	}
	if _, err := fmt.Fprintf(out, "Recorded %d snapshots for %d HPAs to %s in %s\n", total, len(counts), path, time.Since(start).Round(time.Second)); err != nil {
		return err
	}
	if len(changes) == 0 {
		_, err := fmt.Fprintln(out, "\nInteresting changes: none")
		return err
	}
	if _, err := fmt.Fprintln(out, "\nInteresting changes:"); err != nil {
		return err
	}
	for key, entries := range changes {
		if len(entries) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "- %s\n", key); err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintf(out, "  %s\n", entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func runTimelineFromRecord(out io.Writer, opts *options, name, path string) error {
	trace, err := loadRecordedTrace(path, opts.Namespace, name)
	if err != nil {
		return err
	}
	format, _ := outputSelection(outputConfig{report: opts.Report, output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates})
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

func loadRecordedTrace(path, namespace, name string) (*hpaanalysis.TimelineTrace, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read record file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var combined hpaanalysis.TimelineTrace
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var trace hpaanalysis.TimelineTrace
		if err := json.Unmarshal(line, &trace); err != nil {
			return loadRecordedJSONTrace(path, namespace, name)
		}
		if trace.HPAName != name {
			continue
		}
		if namespace != "" && trace.Namespace != namespace {
			continue
		}
		mergeRecordedTrace(&combined, trace)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan record file: %w", err)
	}
	if len(combined.Snapshots) == 0 {
		if lineNo == 0 {
			return loadRecordedJSONTrace(path, namespace, name)
		}
		return nil, fmt.Errorf("record file has no snapshots for %s/%s", namespace, name)
	}
	return &combined, nil
}

func loadRecordedJSONTrace(path, namespace, name string) (*hpaanalysis.TimelineTrace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read record file: %w", err)
	}
	var trace hpaanalysis.TimelineTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, fmt.Errorf("failed to parse record file as JSONL or JSON trace: %w", err)
	}
	if trace.HPAName != name || (namespace != "" && trace.Namespace != namespace) {
		return nil, fmt.Errorf("record file has no snapshots for %s/%s", namespace, name)
	}
	return &trace, nil
}

func mergeRecordedTrace(dst *hpaanalysis.TimelineTrace, src hpaanalysis.TimelineTrace) {
	if dst.HPAName == "" {
		dst.HPAName = src.HPAName
		dst.Namespace = src.Namespace
		dst.Interval = src.Interval
		dst.Start = src.Start
	}
	dst.End = src.End
	dst.Snapshots = append(dst.Snapshots, src.Snapshots...)
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
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read trace file: %w", err)
	}

	var trace hpaanalysis.TimelineTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return fmt.Errorf("failed to parse trace file: %w", err)
	}

	format, _ := outputSelection(outputConfig{report: opts.Report, output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates})
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
