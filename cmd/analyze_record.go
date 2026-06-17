package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type recordAnalysis struct {
	Items []recordAnalysisItem `json:"items" yaml:"items"`
}

type recordAnalysisItem struct {
	Namespace      string   `json:"namespace" yaml:"namespace"`
	Name           string   `json:"name" yaml:"name"`
	Snapshots      int      `json:"snapshots" yaml:"snapshots"`
	DesiredChanges int      `json:"desiredChanges" yaml:"desiredChanges"`
	DirectionFlips int      `json:"directionFlips" yaml:"directionFlips"`
	ReplicaMin     int32    `json:"replicaMin,omitempty" yaml:"replicaMin,omitempty"`
	ReplicaMax     int32    `json:"replicaMax,omitempty" yaml:"replicaMax,omitempty"`
	Level          string   `json:"level" yaml:"level"`
	Suggestions    []string `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
}

func newAnalyzeRecordCommand(opts *options) *cobra.Command {
	var detect string
	cmd := &cobra.Command{
		Use:   "analyze-record FILE",
		Short: "Analyze durable record JSONL for flapping and replica churn",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyzeRecord(cmd.OutOrStdout(), opts, args[0], detect)
		},
	}
	cmd.Flags().StringVar(&detect, "detect", "flapping", "record analysis detector: flapping")
	return cmd
}

func runAnalyzeRecord(out io.Writer, opts *options, path, detect string) error {
	if detect != "" && detect != "flapping" {
		return fmt.Errorf("unsupported detector %q (use flapping)", detect)
	}
	traces, err := loadAllRecordedTraces(path)
	if err != nil {
		return err
	}
	var result recordAnalysis
	for key, trace := range traces {
		item := analyzeTraceFlapping(key, trace)
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

	format, templateStr := outputSelection(outputConfig{output: opts.Output, template: opts.Template, outputTemplates: opts.OutputTemplates})
	return writeOutput(out, format, templateStr, result, func() error {
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
		result[key] = current
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan record file: %w", err)
	}
	return result, nil
}

func analyzeTraceFlapping(_ string, trace hpaanalysis.TimelineTrace) recordAnalysisItem {
	item := recordAnalysisItem{
		Namespace: trace.Namespace,
		Name:      trace.HPAName,
		Snapshots: len(trace.Snapshots),
		Level:     "LOW",
	}
	item.ReplicaMin, item.ReplicaMax = traceReplicaRange(trace)
	var lastDesired int32
	var lastDirection int32
	for i, snap := range trace.Snapshots {
		if i == 0 {
			lastDesired = snap.Desired
			continue
		}
		if snap.Desired == lastDesired {
			continue
		}
		item.DesiredChanges++
		direction := int32(1)
		if snap.Desired < lastDesired {
			direction = -1
		}
		if lastDirection != 0 && direction != lastDirection {
			item.DirectionFlips++
		}
		lastDirection = direction
		lastDesired = snap.Desired
	}
	switch {
	case item.DirectionFlips >= 6 || item.DesiredChanges >= 15:
		item.Level = "CRITICAL"
	case item.DirectionFlips >= 3 || item.DesiredChanges >= 8:
		item.Level = "HIGH"
	case item.DirectionFlips > 0 || item.DesiredChanges >= 4:
		item.Level = "MEDIUM"
	}
	if item.Level != "LOW" {
		item.Suggestions = append(item.Suggestions,
			"review scaleDown stabilization window and configured tolerance",
			"check whether target utilization is too close to normal traffic baseline",
		)
	}
	return item
}
