package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/seasonality"
)

// recordSeasonality is the structured output of `analyze-record --detect seasonality`.
type recordSeasonality struct {
	Items []recordSeasonalityItem `json:"items" yaml:"items"`
}

type recordSeasonalityItem struct {
	Namespace string                `json:"namespace" yaml:"namespace"`
	Name      string                `json:"name" yaml:"name"`
	Snapshots int                   `json:"snapshots" yaml:"snapshots"`
	Analysis  *seasonality.Analysis `json:"analysis" yaml:"analysis"`
}

func runAnalyzeRecordSeasonality(out io.Writer, opts *options, path string, params analyzeRecordOptions) error {
	sopts, err := seasonalityOptions(params)
	if err != nil {
		return err
	}

	traces, err := loadAllRecordedTraces(path)
	if err != nil {
		return err
	}

	var result recordSeasonality
	for _, trace := range traces {
		result.Items = append(result.Items, recordSeasonalityItem{
			Namespace: trace.Namespace,
			Name:      trace.HPAName,
			Snapshots: len(trace.Snapshots),
			Analysis:  seasonality.Analyze(observationsFromTrace(trace), sopts),
		})
	}

	// Detected patterns first, then the highest-confidence findings, so the
	// actionable entries lead the report.
	sort.SliceStable(result.Items, func(i, j int) bool {
		a, b := result.Items[i], result.Items[j]
		if a.Analysis.Detected != b.Analysis.Detected {
			return a.Analysis.Detected
		}
		if a.Analysis.Confidence != b.Analysis.Confidence {
			return a.Analysis.Confidence > b.Analysis.Confidence
		}
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})

	return renderWithOutput(out, opts, result, func(out io.Writer) error {
		return writeSeasonalityText(out, result)
	})

}

// seasonalityOptions maps command flags onto detector options, leaving unset
// flags to the detector's own defaults.
func seasonalityOptions(params analyzeRecordOptions) (seasonality.Options, error) {
	sopts := seasonality.Options{
		LeadTime:      params.leadTime,
		BucketMinutes: params.bucketMinutes,
		MinDays:       params.minDays,
	}
	if params.timezone != "" {
		loc, err := time.LoadLocation(params.timezone)
		if err != nil {
			return sopts, fmt.Errorf("invalid --timezone %q: %w", params.timezone, err)
		}
		sopts.Location = loc
	}
	return sopts, nil
}

// observationsFromTrace projects recorded snapshots onto the detector's input
// type. Snapshots without a timestamp are dropped: the detector's whole basis
// is time-of-day placement, so an undated sample would corrupt the profile.
func observationsFromTrace(trace hpaanalysis.TimelineTrace) []seasonality.Observation {
	out := make([]seasonality.Observation, 0, len(trace.Snapshots))
	for _, snap := range trace.Snapshots {
		if snap.Timestamp.IsZero() {
			continue
		}
		out = append(out, seasonality.Observation{
			Timestamp: snap.Timestamp,
			Desired:   snap.Desired,
		})
	}
	return out
}

func writeSeasonalityText(out io.Writer, result recordSeasonality) error {
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(out, "No recorded HPAs found in the record file.")
		return err
	}

	// Items render into a builder first so the whole report reaches the
	// writer through one checked write.
	var sb strings.Builder
	for i, item := range result.Items {
		if i > 0 {
			sb.WriteByte('\n')
		}
		writeSeasonalityItem(&sb, item)
	}
	_, err := io.WriteString(out, sb.String())
	return err
}

func writeSeasonalityItem(sb *strings.Builder, item recordSeasonalityItem) {
	a := item.Analysis
	switch {
	case a.InsufficientData:
		fmt.Fprintf(sb, "%s/%s: insufficient data (%d snapshots)\n", item.Namespace, item.Name, item.Snapshots)
	case a.Detected:
		fmt.Fprintf(sb, "%s/%s: recurring %s pattern detected (confidence: %s)\n",
			item.Namespace, item.Name, a.Cycle, a.Recommendation.Confidence)
	default:
		fmt.Fprintf(sb, "%s/%s: no recurring pattern detected\n", item.Namespace, item.Name)
	}

	if a.Peak != nil {
		days := "every recorded day"
		if len(a.Peak.Weekdays) > 0 {
			days = strings.Join(a.Peak.Weekdays, ",")
		}
		fmt.Fprintf(sb, "  window:   %s-%s %s (%s, %d of %d days)\n",
			a.Peak.Start, a.Peak.End, a.Timezone, days, a.Peak.DaysMatched, a.Peak.DaysCovered)
		fmt.Fprintf(sb, "  demand:   baseline %.1f -> peak %d replicas\n",
			a.Baseline, a.Peak.PeakDesired)
	}

	if rec := a.Recommendation; rec != nil {
		fmt.Fprintf(sb, "  suggest:  raise minReplicas to %d at %s (%s before the ramp)\n",
			rec.MinReplicas, rec.PrescaleAt, rec.LeadTime)
		fmt.Fprintf(sb, "    cron:    %s\n", rec.CronExpression)
		fmt.Fprintf(sb, "    release: %s\n", rec.ReleaseCronExpression)
		fmt.Fprintln(sb, "    KEDA cron trigger:")
		for line := range strings.SplitSeq(strings.TrimRight(rec.KEDATrigger, "\n"), "\n") {
			fmt.Fprintf(sb, "      %s\n", line)
		}
	}

	for _, note := range a.Notes {
		fmt.Fprintf(sb, "  %s\n", note)
	}
}
