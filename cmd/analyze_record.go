package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
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
}

func newAnalyzeRecordCommand(opts *options) *cobra.Command {
	params := analyzeRecordOptions{}
	cmd := &cobra.Command{
		Use:   "analyze-record FILE",
		Short: "Analyze durable record JSONL for flapping, churn, and recurring demand patterns",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyzeRecord(cmd.OutOrStdout(), opts, args[0], params)
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
	return cmd
}

// Supported analyze-record detectors.
const (
	detectFlapping    = "flapping"
	detectSeasonality = "seasonality"
)

func runAnalyzeRecord(out io.Writer, opts *options, path string, params analyzeRecordOptions) error {
	switch params.detect {
	case "", detectFlapping:
		return runAnalyzeRecordFlapping(out, opts, path)
	case detectSeasonality:
		return runAnalyzeRecordSeasonality(out, opts, path, params)
	default:
		return fmt.Errorf("unsupported detector %q (use %s or %s)",
			params.detect, detectFlapping, detectSeasonality)
	}
}

func runAnalyzeRecordFlapping(out io.Writer, opts *options, path string) error {
	traces, err := loadAllRecordedTraces(path)
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

	format, templateStr := selectOutputFromOptions(opts)
	return render.Format(out, format, templateStr, result, func(out io.Writer) error {
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
	})

}

func loadAllRecordedTraces(path string) (map[string]hpaanalysis.TimelineTrace, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read record file: %w", err)
	}
	defer func() { _ = file.Close() }()

	result := map[string]hpaanalysis.TimelineTrace{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var trace hpaanalysis.TimelineTrace
		if err := json.Unmarshal(line, &trace); err != nil {
			return nil, fmt.Errorf("failed to parse JSONL record: %w", err)
		}
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
			return nil, snapshotLimitError(path)
		}
		result[key] = current
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan record file: %w", err)
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
