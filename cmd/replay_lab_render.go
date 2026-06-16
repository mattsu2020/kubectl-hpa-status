package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// This file holds the rendering helpers for the replay-lab command, split
// from replay_lab.go to separate computation from presentation. The report
// structs and analysis logic remain in replay_lab.go.

func writeReplayLabReport(out io.Writer, opts *options, report replayLabReport) error {
	format, _ := outputSelection(outputConfig{report: opts.report, output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates})
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
	case "markdown", "md":
		return writeReplayLabMarkdown(out, report)
	default:
		return writeReplayLabText(out, report)
	}
}

func writeReplayLabText(out io.Writer, report replayLabReport) error {
	_, _ = fmt.Fprintf(out, "Scenario comparison: %s/%s\n", report.Namespace, report.Name)
	_, _ = fmt.Fprintf(out, "Replay Summary: %s / %s\n\n", report.Name, report.Namespace)
	if len(report.Candidates) > 1 {
		writeReplayPolicyTable(out, report)
		if len(report.Recommendations) > 0 {
			_, _ = fmt.Fprintln(out, "\nRecommended:")
			for _, recommendation := range report.Recommendations {
				_, _ = fmt.Fprintf(out, "  - %s\n", recommendation)
			}
		}
		if len(report.Limitations) > 0 {
			_, _ = fmt.Fprintln(out, "\nLimitations:")
			for _, limitation := range report.Limitations {
				_, _ = fmt.Fprintf(out, "  - %s\n", limitation)
			}
		}
		return nil
	}
	writeReplayLabSummaryText(out, "Current", report.Current)
	if report.CandidateResult != nil {
		_, _ = fmt.Fprintln(out)
		writeReplayLabSummaryText(out, "Proposed", *report.CandidateResult)
	}
	if len(report.ProposedConfig) > 0 {
		_, _ = fmt.Fprintln(out, "\nProposed config:")
		for _, key := range sortedReplayConfigKeys(report.ProposedConfig) {
			_, _ = fmt.Fprintf(out, "  %s: %s\n", key, report.ProposedConfig[key])
		}
	}
	if report.Recommendation != "" {
		_, _ = fmt.Fprintf(out, "\nRecommendation:\n  %s\n", report.Recommendation)
	}
	if report.Impact != nil {
		writeReplayImpactText(out, *report.Impact, report.Current, report.CandidateResult)
	}
	if len(report.Limitations) > 0 {
		_, _ = fmt.Fprintln(out, "\nLimitations:")
		for _, limitation := range report.Limitations {
			_, _ = fmt.Fprintf(out, "  - %s\n", limitation)
		}
	}
	return nil
}

func writeReplayPolicyTable(out io.Writer, report replayLabReport) {
	if len(report.Score) > 0 {
		_, _ = fmt.Fprintf(out, "Score focus: %s\n\n", strings.Join(report.Score, ","))
	}
	_, _ = fmt.Fprintf(out, "%-28s %-9s %-16s %-7s %-7s\n", "Candidate", "SLO risk", "Replica-minutes", "Churn", "Max hit")
	writeReplayPolicyRow(out, "current", report.Current)
	for _, candidate := range report.Candidates {
		writeReplayPolicyRow(out, candidate.Name, candidate.Summary)
	}
}

func writeReplayPolicyRow(out io.Writer, name string, summary replayLabSummary) {
	replicaMinutes := summary.PodHours * 60
	_, _ = fmt.Fprintf(out, "%-28s %-9s %-16.0f %-7d %-7d\n",
		truncateReplayColumn(name, 28),
		replaySLORisk(summary),
		replicaMinutes,
		summary.ScaleEvents,
		summary.MaxReplicasReached,
	)
}

func replaySLORisk(summary replayLabSummary) string {
	switch {
	case summary.MaxReplicasReached > 5 || summary.EstimatedUnderProvision > 3:
		return "high"
	case summary.MaxReplicasReached > 0 || summary.EstimatedUnderProvision > 0:
		return "medium"
	default:
		return "low"
	}
}

func truncateReplayColumn(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func writeReplayLabSummaryText(out io.Writer, title string, summary replayLabSummary) {
	_, _ = fmt.Fprintf(out, "%s:\n", title)
	_, _ = fmt.Fprintf(out, "  snapshots: %d\n", summary.Snapshots)
	_, _ = fmt.Fprintf(out, "  scale events: %d\n", summary.ScaleEvents)
	_, _ = fmt.Fprintf(out, "  direction flips: %d\n", summary.DirectionFlips)
	_, _ = fmt.Fprintf(out, "  peak replicas: %d\n", summary.PeakReplicas)
	if summary.MaxReplicas > 0 {
		_, _ = fmt.Fprintf(out, "  maxReplicas: %d\n", summary.MaxReplicas)
	}
	_, _ = fmt.Fprintf(out, "  max replicas reached: %d\n", summary.MaxReplicasReached)
	_, _ = fmt.Fprintf(out, "  capped duration: %s\n", summary.CappedDuration)
	_, _ = fmt.Fprintf(out, "  estimated under-provision windows: %d\n", summary.EstimatedUnderProvision)
	_, _ = fmt.Fprintf(out, "  estimated pod-hours: %.2f\n", summary.PodHours)
	if summary.ExtraPodHours != 0 {
		_, _ = fmt.Fprintf(out, "  estimated extra pod-hours: %+.2f\n", summary.ExtraPodHours)
	}
	_, _ = fmt.Fprintf(out, "  flapping score: %s\n", summary.FlappingScore)
	if summary.AdditionalWorstCasePods != 0 {
		_, _ = fmt.Fprintf(out, "  additional worst-case pods: %+d\n", summary.AdditionalWorstCasePods)
	}
}

func writeReplayLabMarkdown(out io.Writer, report replayLabReport) error {
	_, _ = fmt.Fprintf(out, "# Scenario comparison: %s/%s\n\n", report.Namespace, report.Name)
	if len(report.Candidates) > 1 {
		writeReplayPolicyMarkdown(out, report)
		if len(report.Limitations) > 0 {
			_, _ = fmt.Fprintln(out, "## Limitations")
			_, _ = fmt.Fprintln(out)
			for _, limitation := range report.Limitations {
				_, _ = fmt.Fprintf(out, "- %s\n", limitation)
			}
		}
		return nil
	}
	writeReplayLabSummaryMarkdown(out, "Current", report.Current)
	if report.CandidateResult != nil {
		writeReplayLabSummaryMarkdown(out, "Proposed", *report.CandidateResult)
	}
	if len(report.ProposedConfig) > 0 {
		_, _ = fmt.Fprintln(out, "## Proposed Config")
		_, _ = fmt.Fprintln(out)
		for _, key := range sortedReplayConfigKeys(report.ProposedConfig) {
			_, _ = fmt.Fprintf(out, "- **%s:** %s\n", key, report.ProposedConfig[key])
		}
		_, _ = fmt.Fprintln(out)
	}
	if report.Recommendation != "" {
		_, _ = fmt.Fprintf(out, "## Recommendation\n\n%s\n\n", report.Recommendation)
	}
	if report.Impact != nil {
		writeReplayImpactMarkdown(out, *report.Impact, report.Current, report.CandidateResult)
	}
	if len(report.Limitations) > 0 {
		_, _ = fmt.Fprintln(out, "## Limitations")
		_, _ = fmt.Fprintln(out)
		for _, limitation := range report.Limitations {
			_, _ = fmt.Fprintf(out, "- %s\n", limitation)
		}
	}
	return nil
}

func writeReplayPolicyMarkdown(out io.Writer, report replayLabReport) {
	if len(report.Score) > 0 {
		_, _ = fmt.Fprintf(out, "**Score focus:** %s\n\n", strings.Join(report.Score, ", "))
	}
	_, _ = fmt.Fprintln(out, "| Candidate | SLO risk | Replica-minutes | Churn | Max hit |")
	_, _ = fmt.Fprintln(out, "| --- | --- | ---: | ---: | ---: |")
	writeReplayPolicyMarkdownRow(out, "current", report.Current)
	for _, candidate := range report.Candidates {
		writeReplayPolicyMarkdownRow(out, candidate.Name, candidate.Summary)
	}
	if len(report.Recommendations) > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "## Recommended")
		_, _ = fmt.Fprintln(out)
		for _, recommendation := range report.Recommendations {
			_, _ = fmt.Fprintf(out, "- %s\n", recommendation)
		}
		_, _ = fmt.Fprintln(out)
	}
}

func writeReplayPolicyMarkdownRow(out io.Writer, name string, summary replayLabSummary) {
	_, _ = fmt.Fprintf(out, "| %s | %s | %.0f | %d | %d |\n",
		name,
		replaySLORisk(summary),
		summary.PodHours*60,
		summary.ScaleEvents,
		summary.MaxReplicasReached,
	)
}

func writeReplayLabSummaryMarkdown(out io.Writer, title string, summary replayLabSummary) {
	_, _ = fmt.Fprintf(out, "## %s\n\n", title)
	_, _ = fmt.Fprintf(out, "- **Snapshots:** %d\n", summary.Snapshots)
	_, _ = fmt.Fprintf(out, "- **Scale events:** %d\n", summary.ScaleEvents)
	_, _ = fmt.Fprintf(out, "- **Direction flips:** %d\n", summary.DirectionFlips)
	_, _ = fmt.Fprintf(out, "- **Peak replicas:** %d\n", summary.PeakReplicas)
	if summary.MaxReplicas > 0 {
		_, _ = fmt.Fprintf(out, "- **maxReplicas:** %d\n", summary.MaxReplicas)
	}
	_, _ = fmt.Fprintf(out, "- **Max replicas reached:** %d\n", summary.MaxReplicasReached)
	_, _ = fmt.Fprintf(out, "- **Capped duration:** %s\n", summary.CappedDuration)
	_, _ = fmt.Fprintf(out, "- **Estimated under-provision windows:** %d\n", summary.EstimatedUnderProvision)
	_, _ = fmt.Fprintf(out, "- **Estimated pod-hours:** %.2f\n", summary.PodHours)
	if summary.ExtraPodHours != 0 {
		_, _ = fmt.Fprintf(out, "- **Estimated extra pod-hours:** %+.2f\n", summary.ExtraPodHours)
	}
	_, _ = fmt.Fprintf(out, "- **Flapping score:** %s\n", summary.FlappingScore)
	if summary.AdditionalWorstCasePods != 0 {
		_, _ = fmt.Fprintf(out, "- **Additional worst-case pods:** %+d\n", summary.AdditionalWorstCasePods)
	}
	_, _ = fmt.Fprintln(out)
}

func sortedReplayConfigKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writeReplayImpactText renders the Impact section comparing current and proposed.
func writeReplayImpactText(out io.Writer, impact ReplayImpact, current replayLabSummary, _ *replayLabSummary) {
	_, _ = fmt.Fprintln(out, "\nImpact:")
	if impact.ScaleEventReductionPct > 0 {
		_, _ = fmt.Fprintf(out, "  - scale churn reduced by %.0f%%\n", impact.ScaleEventReductionPct)
	}
	if impact.PodHoursChangePct != 0 && current.PodHours > 0 {
		_, _ = fmt.Fprintf(out, "  - estimated pod-hours changed by %+.1f%%\n", impact.PodHoursChangePct)
	}
	if impact.UnderProvisionFixed {
		_, _ = fmt.Fprintln(out, "  - under-provision windows eliminated")
	}
	if impact.NoMissedScaleUp {
		_, _ = fmt.Fprintln(out, "  - no missed scale-up detected in recorded window")
	}
	if impact.AdditionalWorstCase > 0 {
		_, _ = fmt.Fprintf(out, "  - additional worst-case pods: +%d\n", impact.AdditionalWorstCase)
	}
}

// writeReplayImpactMarkdown renders the Impact section in Markdown.
func writeReplayImpactMarkdown(out io.Writer, impact ReplayImpact, current replayLabSummary, _ *replayLabSummary) {
	_, _ = fmt.Fprintln(out, "## Impact")
	_, _ = fmt.Fprintln(out)
	if impact.ScaleEventReductionPct > 0 {
		_, _ = fmt.Fprintf(out, "- Scale churn reduced by **%.0f%%**\n", impact.ScaleEventReductionPct)
	}
	if impact.PodHoursChangePct != 0 && current.PodHours > 0 {
		_, _ = fmt.Fprintf(out, "- Estimated pod-hours changed by **%+.1f%%**\n", impact.PodHoursChangePct)
	}
	if impact.UnderProvisionFixed {
		_, _ = fmt.Fprintln(out, "- Under-provision windows **eliminated**")
	}
	if impact.NoMissedScaleUp {
		_, _ = fmt.Fprintln(out, "- No missed scale-up detected in recorded window")
	}
	if impact.AdditionalWorstCase > 0 {
		_, _ = fmt.Fprintf(out, "- Additional worst-case pods: **+%d**\n", impact.AdditionalWorstCase)
	}
	_, _ = fmt.Fprintln(out)
}
