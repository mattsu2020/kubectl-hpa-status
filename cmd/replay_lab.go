package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/yaml"
)

type replayLabReport struct {
	Namespace       string            `json:"namespace" yaml:"namespace"`
	Name            string            `json:"name" yaml:"name"`
	Record          string            `json:"record" yaml:"record"`
	Candidate       string            `json:"candidate,omitempty" yaml:"candidate,omitempty"`
	Current         replayLabSummary  `json:"current" yaml:"current"`
	CandidateResult *replayLabSummary `json:"candidateResult,omitempty" yaml:"candidateResult,omitempty"`
	Recommendation  string            `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	Limitations     []string          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type replayLabSummary struct {
	Snapshots               int    `json:"snapshots" yaml:"snapshots"`
	ScaleEvents             int    `json:"scaleEvents" yaml:"scaleEvents"`
	DirectionFlips          int    `json:"directionFlips" yaml:"directionFlips"`
	PeakReplicas            int32  `json:"peakReplicas" yaml:"peakReplicas"`
	MaxReplicas             int32  `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	MaxReplicasReached      int    `json:"maxReplicasReached" yaml:"maxReplicasReached"`
	EstimatedUnderProvision int    `json:"estimatedUnderProvisionWindows" yaml:"estimatedUnderProvisionWindows"`
	AdditionalWorstCasePods int32  `json:"additionalWorstCasePods,omitempty" yaml:"additionalWorstCasePods,omitempty"`
	FlappingScore           string `json:"flappingScore" yaml:"flappingScore"`
}

func runReplayLab(out io.Writer, opts *options, name, recordPath, candidatePath string) error {
	trace, err := loadRecordedTrace(recordPath, opts.namespace, name)
	if err != nil {
		return err
	}
	report := replayLabReport{
		Namespace: trace.Namespace,
		Name:      trace.HPAName,
		Record:    recordPath,
		Current:   summarizeReplayTrace(*trace, 0),
		Limitations: []string{
			"[estimated] record snapshots do not expose controller-internal recommendations, raw metric windows, or tolerance decisions.",
			"[estimated] candidate replay clamps recorded desiredReplicas to candidate min/maxReplicas; metric target changes are not re-derived from raw metrics.",
		},
	}
	if candidatePath != "" {
		candidate, loadErr := loadCandidateHPA(candidatePath)
		if loadErr != nil {
			return loadErr
		}
		candidateTrace := applyCandidateBounds(*trace, candidate)
		candidateSummary := summarizeReplayTrace(candidateTrace, candidate.Spec.MaxReplicas)
		candidateSummary.AdditionalWorstCasePods = candidate.Spec.MaxReplicas - report.Current.PeakReplicas
		report.Candidate = candidatePath
		report.CandidateResult = &candidateSummary
		report.Recommendation = replayLabRecommendation(report.Current, candidateSummary)
	}
	return writeReplayLabReport(out, opts, report)
}

func loadCandidateHPA(path string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read candidate HPA %s: %w", path, err)
	}
	var hpa autoscalingv2.HorizontalPodAutoscaler
	if err := yaml.Unmarshal(data, &hpa); err != nil {
		return nil, fmt.Errorf("failed to parse candidate HPA %s: %w", path, err)
	}
	if hpa.Spec.MaxReplicas <= 0 {
		return nil, fmt.Errorf("candidate HPA %s has invalid spec.maxReplicas", path)
	}
	return &hpa, nil
}

func summarizeReplayTrace(trace hpaanalysis.TimelineTrace, maxReplicas int32) replayLabSummary {
	summary := replayLabSummary{Snapshots: len(trace.Snapshots), MaxReplicas: maxReplicas}
	var lastDesired int32
	var lastDirection int32
	for i, snap := range trace.Snapshots {
		if snap.Desired > summary.PeakReplicas {
			summary.PeakReplicas = snap.Desired
		}
		if maxReplicas > 0 && snap.Desired >= maxReplicas {
			summary.MaxReplicasReached++
		}
		if snap.Health == "LIMITED" || hasTimelineCondition(snap, "ScalingLimited", "True") {
			summary.EstimatedUnderProvision++
		}
		if i == 0 {
			lastDesired = snap.Desired
			continue
		}
		if snap.Desired == lastDesired {
			continue
		}
		summary.ScaleEvents++
		direction := int32(1)
		if snap.Desired < lastDesired {
			direction = -1
		}
		if lastDirection != 0 && direction != lastDirection {
			summary.DirectionFlips++
		}
		lastDirection = direction
		lastDesired = snap.Desired
	}
	summary.FlappingScore = replayFlappingScore(summary.ScaleEvents, summary.DirectionFlips)
	return summary
}

func hasTimelineCondition(snapshot hpaanalysis.TimelineSnapshot, conditionType, status string) bool {
	for _, condition := range snapshot.Conditions {
		if condition.Type == conditionType && condition.Status == status {
			return true
		}
	}
	return false
}

func applyCandidateBounds(trace hpaanalysis.TimelineTrace, candidate *autoscalingv2.HorizontalPodAutoscaler) hpaanalysis.TimelineTrace {
	if candidate == nil {
		return trace
	}
	minReplicas := int32(1)
	if candidate.Spec.MinReplicas != nil {
		minReplicas = *candidate.Spec.MinReplicas
	}
	maxReplicas := candidate.Spec.MaxReplicas
	out := trace
	out.Snapshots = append([]hpaanalysis.TimelineSnapshot(nil), trace.Snapshots...)
	for i := range out.Snapshots {
		desired := out.Snapshots[i].Desired
		if desired < minReplicas {
			desired = minReplicas
		}
		if desired > maxReplicas {
			desired = maxReplicas
			out.Snapshots[i].Health = "LIMITED"
		}
		out.Snapshots[i].Desired = desired
	}
	return out
}

func replayFlappingScore(scaleEvents, directionFlips int) string {
	switch {
	case directionFlips >= 6 || scaleEvents >= 15:
		return "high"
	case directionFlips >= 3 || scaleEvents >= 8:
		return "medium"
	case directionFlips > 0 || scaleEvents >= 4:
		return "low"
	default:
		return "none"
	}
}

func replayLabRecommendation(current, candidate replayLabSummary) string {
	var parts []string
	if current.ScaleEvents > 0 && candidate.ScaleEvents < current.ScaleEvents {
		reduction := float64(current.ScaleEvents-candidate.ScaleEvents) / float64(current.ScaleEvents) * 100
		parts = append(parts, fmt.Sprintf("Candidate reduces visible replica churn by ~%.0f%%", reduction))
	}
	if candidate.EstimatedUnderProvision < current.EstimatedUnderProvision {
		parts = append(parts, fmt.Sprintf("estimated under-provision windows drop from %d to %d", current.EstimatedUnderProvision, candidate.EstimatedUnderProvision))
	}
	if candidate.AdditionalWorstCasePods > 0 {
		parts = append(parts, fmt.Sprintf("but increases peak capacity by +%d pod(s)", candidate.AdditionalWorstCasePods))
	}
	if len(parts) == 0 {
		return "Candidate does not materially improve visible churn or maxReplicas pressure in this record."
	}
	return strings.Join(parts, "; ") + "."
}

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
	_, _ = fmt.Fprintf(out, "Replay Summary: %s / %s\n\n", report.Name, report.Namespace)
	writeReplayLabSummaryText(out, "Current HPA", report.Current)
	if report.CandidateResult != nil {
		_, _ = fmt.Fprintln(out)
		writeReplayLabSummaryText(out, "Candidate HPA", *report.CandidateResult)
	}
	if report.Recommendation != "" {
		_, _ = fmt.Fprintf(out, "\nRecommendation:\n  %s\n", report.Recommendation)
	}
	if len(report.Limitations) > 0 {
		_, _ = fmt.Fprintln(out, "\nLimitations:")
		for _, limitation := range report.Limitations {
			_, _ = fmt.Fprintf(out, "  - %s\n", limitation)
		}
	}
	return nil
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
	_, _ = fmt.Fprintf(out, "  estimated under-provision windows: %d\n", summary.EstimatedUnderProvision)
	_, _ = fmt.Fprintf(out, "  flapping score: %s\n", summary.FlappingScore)
	if summary.AdditionalWorstCasePods != 0 {
		_, _ = fmt.Fprintf(out, "  additional worst-case pods: %+d\n", summary.AdditionalWorstCasePods)
	}
}

func writeReplayLabMarkdown(out io.Writer, report replayLabReport) error {
	_, _ = fmt.Fprintf(out, "# Replay Summary: %s/%s\n\n", report.Namespace, report.Name)
	writeReplayLabSummaryMarkdown(out, "Current HPA", report.Current)
	if report.CandidateResult != nil {
		writeReplayLabSummaryMarkdown(out, "Candidate HPA", *report.CandidateResult)
	}
	if report.Recommendation != "" {
		_, _ = fmt.Fprintf(out, "## Recommendation\n\n%s\n\n", report.Recommendation)
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
	_, _ = fmt.Fprintf(out, "- **Estimated under-provision windows:** %d\n", summary.EstimatedUnderProvision)
	_, _ = fmt.Fprintf(out, "- **Flapping score:** %s\n", summary.FlappingScore)
	if summary.AdditionalWorstCasePods != 0 {
		_, _ = fmt.Fprintf(out, "- **Additional worst-case pods:** %+d\n", summary.AdditionalWorstCasePods)
	}
	_, _ = fmt.Fprintln(out)
}
