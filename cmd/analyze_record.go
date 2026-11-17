package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/recordio"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/flapping"
)

type recordAnalysis struct {
	Items []flapping.TraceReport `json:"items" yaml:"items"`
}

// analyzeRecordOptions carries the detector selection and the tuning knobs
// that only apply to some detectors, so adding a detector does not keep
// widening the runAnalyzeRecord signature.
type analyzeRecordOptions struct {
	detect        string
	timezone      string
	leadTime      time.Duration
	bucketMinutes int
	minDays       int
	signal        string
	hpaName       string
	prom          prometheusImportFlags
}

func newAnalyzeRecordCommand(opts *options) *cobra.Command {
	params := analyzeRecordOptions{}
	cmd := &cobra.Command{
		Use:   "analyze-record [FILE]",
		Short: "Analyze durable record JSONL or Prometheus history for flapping, churn, and recurring demand patterns",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) > 0 {
				path = args[0]
			}
			return runAnalyzeRecordWithContext(cmd.Context(), cmd.OutOrStdout(), opts, path, params)
		},
	}
	cmd.Flags().StringVar(&params.detect, "detect", detectFlapping,
		"record analysis detector: flapping, seasonality")
	cmd.Flags().StringVar(&params.timezone, "timezone", "",
		"IANA timezone for seasonality schedules (default: local timezone)")
	cmd.Flags().DurationVar(&params.leadTime, "lead-time", 0,
		"how far ahead of a detected ramp to pre-scale (seasonality; default 15m)")
	cmd.Flags().IntVar(&params.bucketMinutes, "bucket-minutes", 0,
		"time-of-day resolution in minutes for seasonality detection (default 30)")
	cmd.Flags().IntVar(&params.minDays, "min-days", 0,
		"minimum distinct days required to claim daily periodicity (default 2)")
	cmd.Flags().StringVar(&params.signal, "signal", "auto",
		"demand signal for seasonality detection: auto, metric, replicas (default auto)")
	cmd.Flags().StringVar(&params.hpaName, "name", "",
		"HPA name when importing history with --from-prometheus")
	addPrometheusImportFlags(cmd, &params.prom, true)
	return cmd
}

// Supported analyze-record detectors.
const (
	detectFlapping    = "flapping"
	detectSeasonality = "seasonality"
)

func runAnalyzeRecord(out io.Writer, opts *options, path string, params analyzeRecordOptions) error {
	return runAnalyzeRecordWithContext(context.Background(), out, opts, path, params)
}

func runAnalyzeRecordWithContext(ctx context.Context, out io.Writer, opts *options, path string, params analyzeRecordOptions) error {
	switch params.detect {
	case "", detectFlapping:
		return runAnalyzeRecordFlapping(ctx, out, opts, path, params)
	case detectSeasonality:
		return runAnalyzeRecordSeasonality(ctx, out, opts, path, params)
	default:
		return fmt.Errorf("unsupported detector %q (use %s or %s)",
			params.detect, detectFlapping, detectSeasonality)
	}
}

func loadAnalyzeTraces(ctx context.Context, opts *options, path string, params analyzeRecordOptions) (map[string]hpaanalysis.TimelineTrace, error) {
	if params.prom.requested() {
		if path != "" {
			return nil, fmt.Errorf("record FILE argument and --from-prometheus cannot be used together")
		}
		name := strings.TrimSpace(params.hpaName)
		if name == "" {
			return nil, fmt.Errorf("--name is required when using --from-prometheus")
		}
		imported, err := loadTraceFromPrometheus(ctx, opts, name, params.prom.Since, params.prom)
		if err != nil {
			return nil, err
		}
		key := imported.Trace.Namespace + "/" + imported.Trace.HPAName
		return map[string]hpaanalysis.TimelineTrace{key: imported.Trace}, nil
	}
	if path == "" {
		return nil, fmt.Errorf("record FILE argument or --from-prometheus is required")
	}
	return loadAllRecordedTraces(path)
}

func runAnalyzeRecordFlapping(ctx context.Context, out io.Writer, opts *options, path string, params analyzeRecordOptions) error {
	traces, err := loadAnalyzeTraces(ctx, opts, path, params)
	if err != nil {
		return err
	}
	var result recordAnalysis
	for _, trace := range traces {
		item := traceFlappingReport(trace)
		if item.DesiredChanges > 0 || item.DirectionFlips > 0 {
			result.Items = append(result.Items, item)
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].DirectionFlips != result.Items[j].DirectionFlips {
			return result.Items[i].DirectionFlips > result.Items[j].DirectionFlips
		}
		return result.Items[i].DesiredChanges > result.Items[j].DesiredChanges
	})

	return writeRecordAnalysis(out, opts, result)
}

func writeRecordAnalysis(out io.Writer, opts *options, result recordAnalysis) error {
	return renderWithOutput(out, opts, result, func(out io.Writer) error {
		var buffer strings.Builder
		if err := writeAnalyzeRecordFlappingText(&buffer, result); err != nil {
			return err
		}
		_, err := io.WriteString(out, buffer.String())
		return err
	})
}

func writeAnalyzeRecordFlappingText(out io.Writer, result recordAnalysis) error {
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(out, "No HPA flapping detected.")
		return err
	}
	_, _ = fmt.Fprintln(out, "Detected HPA flapping:")
	for _, item := range result.Items {
		_, _ = fmt.Fprintf(out, "- %s/%s changed desiredReplicas %d times across %d snapshots\n", item.Namespace, item.Name, item.DesiredChanges, item.Snapshots)
		if item.DirectionFlips > 0 {
			_, _ = fmt.Fprintf(out, "  scale direction alternated %d times\n", item.DirectionFlips)
		}
		_, _ = fmt.Fprintf(out, "  level: %s\n", item.Level)
		for _, suggestion := range item.Suggestions {
			_, _ = fmt.Fprintf(out, "  suggestion: %s\n", suggestion)
		}
	}
	return nil
}

func loadAllRecordedTraces(path string) (map[string]hpaanalysis.TimelineTrace, error) {
	result := map[string]hpaanalysis.TimelineTrace{}
	_, err := recordio.ScanTraces(path, func(trace hpaanalysis.TimelineTrace) error {
		key := trace.Namespace + "/" + trace.HPAName
		current := result[key]
		if current.HPAName == "" {
			current.HPAName = trace.HPAName
			current.Namespace = trace.Namespace
			current.Interval = trace.Interval
			current.Start = trace.Start
		}
		current.End = trace.End
		current.Snapshots = append(current.Snapshots, trace.Snapshots...)
		if len(current.Snapshots) > maxSnapshotsPerTrace {
			return snapshotLimitError(path)
		}
		result[key] = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// traceFlappingReport adapts a recorded trace to the flapping package's
// TraceInput and runs the recorded-trace flapping detector. The thresholds
// and level classification live in pkg/hpa/flapping so the analyze-record and
// flap commands share one implementation.
func traceFlappingReport(trace hpaanalysis.TimelineTrace) flapping.TraceReport {
	desired := make([]int32, len(trace.Snapshots))
	for i, snap := range trace.Snapshots {
		desired[i] = snap.Desired
	}
	return flapping.AnalyzeTrace(flapping.TraceInput{
		Namespace: trace.Namespace,
		Name:      trace.HPAName,
		Desired:   desired,
	})
}
