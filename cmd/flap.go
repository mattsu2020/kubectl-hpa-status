package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type flapReport struct {
	Namespace       string                                `json:"namespace" yaml:"namespace"`
	Name            string                                `json:"name" yaml:"name"`
	Source          string                                `json:"source" yaml:"source"`
	Snapshots       int                                   `json:"snapshots,omitempty" yaml:"snapshots,omitempty"`
	ScaleEvents     int                                   `json:"scaleEvents" yaml:"scaleEvents"`
	DirectionFlips  int                                   `json:"directionFlips" yaml:"directionFlips"`
	ReplicaMin      int32                                 `json:"replicaMin,omitempty" yaml:"replicaMin,omitempty"`
	ReplicaMax      int32                                 `json:"replicaMax,omitempty" yaml:"replicaMax,omitempty"`
	Level           string                                `json:"level" yaml:"level"`
	Recommendations []string                              `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	Prevention      *hpaanalysis.FlappingPreventionReport `json:"prevention,omitempty" yaml:"prevention,omitempty"`
	Diagnosis       *hpaanalysis.FlappingDiagnosis        `json:"diagnosis,omitempty" yaml:"diagnosis,omitempty"`
}

func newFlapCommand(opts *options) *cobra.Command {
	var since time.Duration
	var fromRecord string
	cmd := &cobra.Command{
		Use:               "flap NAME",
		Short:             "Detect HPA scale flapping from events or recorded history",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if fromRecord != "" {
				return runFlapFromRecord(cmd.OutOrStdout(), opts, args[0], fromRecord)
			}
			return runFlapLive(cmd.Context(), cmd.OutOrStdout(), opts, args[0], since)
		},
	}
	cmd.Flags().DurationVar(&since, "since", 6*time.Hour, "look back over recent HPA events")
	cmd.Flags().StringVar(&fromRecord, "from-record", "", "read durable JSONL/JSON trace written by record")
	return cmd
}

func runFlapLive(ctx context.Context, out io.Writer, opts *options, name string, since time.Duration) error {
	client, hpa, err := lookupHPA(ctx, opts, name)
	if err != nil {
		return err
	}
	if since <= 0 {
		since = 6 * time.Hour
	}
	coreEvents, err := kube.FetchRecentHPAEventsSince(ctx, client.Interface, hpa.Namespace, hpa.Name, time.Now().Add(-since))
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	events := hpaanalysis.EventsFromCore(coreEvents)
	prevention := hpaanalysis.AnalyzeFlappingPrevention(events, hpa)
	diagnosis := hpaanalysis.DiagnoseFlapping(events, hpa)
	report := flapReport{
		Namespace:       hpa.Namespace,
		Name:            hpa.Name,
		Source:          fmt.Sprintf("events since %s", since),
		ScaleEvents:     countRescaleEvents(events),
		Level:           "LOW",
		Recommendations: []string{"record HPA history with `kubectl hpa_status record` if Events have expired"},
		Prevention:      prevention,
		Diagnosis:       diagnosis,
	}
	if prevention != nil {
		report.DirectionFlips = prevention.CurrentDirectionFlips
		report.Level = flapLevel(report.ScaleEvents, report.DirectionFlips)
		report.Recommendations = flapRecommendations(report.Level)
	}
	return writeFlapReport(out, opts, report)
}

func runFlapFromRecord(out io.Writer, opts *options, name, path string) error {
	trace, err := loadRecordedTrace(path, opts.Namespace, name)
	if err != nil {
		return err
	}
	item := analyzeTraceFlapping("", *trace)
	report := flapReport{
		Namespace:       item.Namespace,
		Name:            item.Name,
		Source:          path,
		Snapshots:       item.Snapshots,
		ScaleEvents:     item.DesiredChanges,
		DirectionFlips:  item.DirectionFlips,
		ReplicaMin:      item.ReplicaMin,
		ReplicaMax:      item.ReplicaMax,
		Level:           item.Level,
		Recommendations: item.Suggestions,
	}
	if report.Level == "LOW" {
		report.Recommendations = []string{"no sustained flapping pattern detected in recorded desiredReplicas"}
	}
	return writeFlapReport(out, opts, report)
}

func writeFlapReport(out io.Writer, opts *options, report flapReport) error {
	format, _ := selectOutputFromOptions(opts)
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case "yaml":
		data, err := yaml.Marshal(report)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	default:
		theme := style.NewTheme(shouldColorize(opts.Color, out))
		_, _ = fmt.Fprintf(out, "Flapping Analysis: %s/%s\n", report.Namespace, report.Name)
		_, _ = fmt.Fprintf(out, "  source: %s\n", report.Source)
		if report.Snapshots > 0 {
			_, _ = fmt.Fprintf(out, "  snapshots: %d\n", report.Snapshots)
		}
		_, _ = fmt.Fprintf(out, "  scale events: %d\n", report.ScaleEvents)
		_, _ = fmt.Fprintf(out, "  direction changes: %d\n", report.DirectionFlips)
		if report.ReplicaMin > 0 || report.ReplicaMax > 0 {
			_, _ = fmt.Fprintf(out, "  replica range: %d -> %d\n", report.ReplicaMin, report.ReplicaMax)
		}
		_, _ = fmt.Fprintf(out, "  level: %s\n", theme.SummaryColor(report.Level))
		if report.Diagnosis != nil && report.Diagnosis.Detected {
			writeFlapDiagnosisText(out, report.Diagnosis)
		}
		if report.Prevention != nil {
			writeFlapPreventionText(out, report.Prevention)
		}
		if len(report.Recommendations) > 0 {
			_, _ = fmt.Fprintln(out, "\nRecommendations:")
			for _, rec := range report.Recommendations {
				_, _ = fmt.Fprintf(out, "  - %s\n", rec)
			}
		}
		return nil
	}
}

func writeFlapDiagnosisText(out io.Writer, d *hpaanalysis.FlappingDiagnosis) {
	_, _ = fmt.Fprintln(out, "\nFlapping Diagnosis:")
	_, _ = fmt.Fprintf(out, "  severity: %s\n", d.Severity)
	if d.Pattern != "" {
		_, _ = fmt.Fprintf(out, "  pattern: %s\n", d.Pattern)
	}
	_, _ = fmt.Fprintf(out, "  direction flips: %d in %ds\n", d.FlipCount, d.WindowSeconds)
	if len(d.EstimatedCauses) > 0 {
		_, _ = fmt.Fprintln(out, "\n  Estimated Causes:")
		for _, cause := range d.EstimatedCauses {
			_, _ = fmt.Fprintf(out, "    - [%s] %s (confidence: %s)\n", cause.Type, cause.Description, cause.Confidence)
		}
	}
	if len(d.Recommendations) > 0 {
		_, _ = fmt.Fprintln(out, "\n  Recommended Fixes:")
		for _, fix := range d.Recommendations {
			_, _ = fmt.Fprintf(out, "    - %s\n", fix.Action)
			_, _ = fmt.Fprintf(out, "      %s\n", fix.Rationale)
			if fix.Patch != "" {
				_, _ = fmt.Fprintf(out, "      patch: %s\n", fix.Patch)
			}
		}
	}
	if d.EventTTLLimitation != "" {
		_, _ = fmt.Fprintf(out, "\n  Note: %s\n", d.EventTTLLimitation)
	}
}

func writeFlapPreventionText(out io.Writer, report *hpaanalysis.FlappingPreventionReport) {
	_, _ = fmt.Fprintln(out, "\nFlapping Prevention:")
	_, _ = fmt.Fprintf(out, "  Current stabilization window: %ds\n", report.CurrentWindow)
	_, _ = fmt.Fprintf(out, "  Direction flips: %d\n", report.CurrentDirectionFlips)
	if report.ObservationWindow != "" {
		_, _ = fmt.Fprintf(out, "  Observation window: %s\n", report.ObservationWindow)
	}
	if len(report.Recommendations) > 0 {
		_, _ = fmt.Fprintln(out, "\n  Recommendations:")
		for _, rec := range report.Recommendations {
			_, _ = fmt.Fprintf(out, "    - Window %ds: flips %d -> %d (%.0f%% reduction, confidence: %s)\n",
				rec.WindowSeconds, report.CurrentDirectionFlips, rec.EstimatedDirectionFlips, rec.EstimatedFlapReduction, rec.Confidence)
		}
	}
	if report.Summary != "" {
		_, _ = fmt.Fprintf(out, "\n  %s\n", report.Summary)
	}
}

func countRescaleEvents(events []hpaanalysis.Event) int {
	count := 0
	for _, event := range events {
		if event.Reason == "SuccessfulRescale" {
			count++
		}
	}
	return count
}

func flapLevel(scaleEvents, flips int) string {
	switch {
	case flips >= 6 || scaleEvents >= 15:
		return "CRITICAL"
	case flips >= 3 || scaleEvents >= 8:
		return "HIGH"
	case flips > 0 || scaleEvents >= 4:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

func flapRecommendations(level string) []string {
	if level == "LOW" {
		return []string{"no sustained flapping pattern detected in visible HPA events"}
	}
	return []string{
		"review scaleDown.stabilizationWindowSeconds against workload warm-up time",
		"check whether CPU or custom metrics frequently cross the target threshold",
		"consider a less aggressive scaleDown policy such as Percent 10 per 60s",
	}
}

func traceReplicaRange(trace hpaanalysis.TimelineTrace) (int32, int32) {
	if len(trace.Snapshots) == 0 {
		return 0, 0
	}
	values := make([]int, 0, len(trace.Snapshots))
	for _, snap := range trace.Snapshots {
		values = append(values, int(snap.Desired))
	}
	sort.Ints(values)
	return int32(values[0]), int32(values[len(values)-1])
}
