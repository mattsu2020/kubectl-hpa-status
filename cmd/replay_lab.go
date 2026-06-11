package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/yaml"
)

type replayLabReport struct {
	Namespace       string            `json:"namespace" yaml:"namespace"`
	Name            string            `json:"name" yaml:"name"`
	Record          string            `json:"record" yaml:"record"`
	Candidate       string            `json:"candidate,omitempty" yaml:"candidate,omitempty"`
	ProposedConfig  map[string]string `json:"proposedConfig,omitempty" yaml:"proposedConfig,omitempty"`
	Current         replayLabSummary  `json:"current" yaml:"current"`
	CandidateResult *replayLabSummary `json:"candidateResult,omitempty" yaml:"candidateResult,omitempty"`
	Recommendation  string            `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	Limitations     []string          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type replayLabSummary struct {
	Snapshots               int     `json:"snapshots" yaml:"snapshots"`
	ScaleEvents             int     `json:"scaleEvents" yaml:"scaleEvents"`
	DirectionFlips          int     `json:"directionFlips" yaml:"directionFlips"`
	PeakReplicas            int32   `json:"peakReplicas" yaml:"peakReplicas"`
	MaxReplicas             int32   `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	MaxReplicasReached      int     `json:"maxReplicasReached" yaml:"maxReplicasReached"`
	CappedDurationSeconds   int64   `json:"cappedDurationSeconds" yaml:"cappedDurationSeconds"`
	CappedDuration          string  `json:"cappedDuration" yaml:"cappedDuration"`
	EstimatedUnderProvision int     `json:"estimatedUnderProvisionWindows" yaml:"estimatedUnderProvisionWindows"`
	PodHours                float64 `json:"podHours" yaml:"podHours"`
	ExtraPodHours           float64 `json:"extraPodHours,omitempty" yaml:"extraPodHours,omitempty"`
	AdditionalWorstCasePods int32   `json:"additionalWorstCasePods,omitempty" yaml:"additionalWorstCasePods,omitempty"`
	FlappingScore           string  `json:"flappingScore" yaml:"flappingScore"`
}

type replayCandidateConfig struct {
	MinReplicas                   *int32
	MaxReplicas                   int32
	ScaleDownStabilizationSeconds int32
	Proposed                      map[string]string
}

func runReplayLab(out io.Writer, opts *options, name, recordPath, candidatePath string, overrides map[string]string) error {
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
			"[estimated] candidate replay applies min/maxReplicas and scaleDown stabilization to recorded desiredReplicas; metric target changes are not re-derived from raw metrics.",
		},
	}
	if candidatePath != "" || len(overrides) > 0 {
		candidate, loadErr := buildReplayCandidate(candidatePath, overrides)
		if loadErr != nil {
			return loadErr
		}
		candidateTrace := applyReplayCandidate(*trace, candidate)
		candidateSummary := summarizeReplayTraceWithDemand(candidateTrace, candidate.MaxReplicas, trace)
		candidateSummary.AdditionalWorstCasePods = candidate.MaxReplicas - report.Current.PeakReplicas
		candidateSummary.ExtraPodHours = candidateSummary.PodHours - report.Current.PodHours
		report.Candidate = candidatePath
		report.ProposedConfig = candidate.Proposed
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

func buildReplayCandidate(path string, overrides map[string]string) (replayCandidateConfig, error) {
	cfg := replayCandidateConfig{Proposed: map[string]string{}}
	if path != "" {
		hpa, err := loadCandidateHPA(path)
		if err != nil {
			return cfg, err
		}
		cfg.MinReplicas = hpa.Spec.MinReplicas
		cfg.MaxReplicas = hpa.Spec.MaxReplicas
		cfg.Proposed["candidate"] = path
		if hpa.Spec.MinReplicas != nil {
			cfg.Proposed["minReplicas"] = fmt.Sprint(*hpa.Spec.MinReplicas)
		}
		cfg.Proposed["maxReplicas"] = fmt.Sprint(hpa.Spec.MaxReplicas)
		if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
			cfg.ScaleDownStabilizationSeconds = *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
			cfg.Proposed["scaleDown.stabilizationWindowSeconds"] = fmt.Sprint(cfg.ScaleDownStabilizationSeconds)
		}
	}
	for key, value := range overrides {
		if err := applyReplayCandidateOverride(&cfg, key, value); err != nil {
			return cfg, err
		}
		cfg.Proposed[key] = value
	}
	if cfg.MaxReplicas <= 0 {
		return cfg, fmt.Errorf("replay --from-record with --set requires maxReplicas, or provide --candidate")
	}
	return cfg, nil
}

func applyReplayCandidateOverride(cfg *replayCandidateConfig, key, value string) error {
	switch key {
	case "minReplicas":
		parsed, err := parseReplayInt32(key, value)
		if err != nil {
			return err
		}
		cfg.MinReplicas = &parsed
	case "maxReplicas":
		parsed, err := parseReplayInt32(key, value)
		if err != nil {
			return err
		}
		if parsed <= 0 {
			return fmt.Errorf("maxReplicas must be greater than zero")
		}
		cfg.MaxReplicas = parsed
	case "scaleDown.stabilizationWindowSeconds":
		parsed, err := parseReplayInt32(key, value)
		if err != nil {
			return err
		}
		if parsed < 0 {
			return fmt.Errorf("scaleDown.stabilizationWindowSeconds must be non-negative")
		}
		cfg.ScaleDownStabilizationSeconds = parsed
	case "scaleUp.tolerance", "scaleDown.tolerance", "cpu.targetAverageUtilization", "memory.targetAverageUtilization":
		// Accepted for report completeness. Recorded snapshots do not contain
		// raw metric windows, so tolerance and target changes cannot be
		// replayed safely here.
		return nil
	default:
		return fmt.Errorf("unsupported replay --set %q", key)
	}
	return nil
}

func parseReplayInt32(key, value string) (int32, error) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", key, value, err)
	}
	return int32(parsed), nil
}

func summarizeReplayTrace(trace hpaanalysis.TimelineTrace, maxReplicas int32) replayLabSummary {
	return summarizeReplayTraceWithDemand(trace, maxReplicas, nil)
}

func summarizeReplayTraceWithDemand(trace hpaanalysis.TimelineTrace, maxReplicas int32, demandTrace *hpaanalysis.TimelineTrace) replayLabSummary {
	summary := replayLabSummary{Snapshots: len(trace.Snapshots), MaxReplicas: maxReplicas}
	var lastDesired int32
	var lastDirection int32
	for i, snap := range trace.Snapshots {
		seconds := replaySnapshotDurationSeconds(trace, i)
		if snap.Desired > summary.PeakReplicas {
			summary.PeakReplicas = snap.Desired
		}
		capped := maxReplicas > 0 && snap.Desired >= maxReplicas
		if demandTrace != nil && i < len(demandTrace.Snapshots) && maxReplicas > 0 {
			capped = demandTrace.Snapshots[i].Desired > maxReplicas
		}
		if capped {
			summary.MaxReplicasReached++
			summary.CappedDurationSeconds += seconds
		}
		if snap.Health == "LIMITED" || hasTimelineCondition(snap, "ScalingLimited", "True") {
			summary.EstimatedUnderProvision++
			if maxReplicas == 0 {
				summary.CappedDurationSeconds += seconds
			}
		}
		summary.PodHours += float64(snap.Desired) * float64(seconds) / 3600.0
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
	summary.CappedDuration = formatReplayDuration(time.Duration(summary.CappedDurationSeconds) * time.Second)
	summary.FlappingScore = replayFlappingScore(summary.ScaleEvents, summary.DirectionFlips)
	return summary
}

func replaySnapshotDurationSeconds(trace hpaanalysis.TimelineTrace, index int) int64 {
	if index >= 0 && index+1 < len(trace.Snapshots) {
		if d := trace.Snapshots[index+1].Timestamp.Sub(trace.Snapshots[index].Timestamp); d > 0 {
			return int64(d.Seconds())
		}
	}
	if trace.Interval > 0 {
		return int64(trace.Interval.Seconds())
	}
	return 0
}

func formatReplayDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func hasTimelineCondition(snapshot hpaanalysis.TimelineSnapshot, conditionType, status string) bool {
	for _, condition := range snapshot.Conditions {
		if condition.Type == conditionType && condition.Status == status {
			return true
		}
	}
	return false
}

func applyReplayCandidate(trace hpaanalysis.TimelineTrace, candidate replayCandidateConfig) hpaanalysis.TimelineTrace {
	minReplicas := int32(1)
	if candidate.MinReplicas != nil {
		minReplicas = *candidate.MinReplicas
	}
	maxReplicas := candidate.MaxReplicas
	out := trace
	out.Snapshots = append([]hpaanalysis.TimelineSnapshot(nil), trace.Snapshots...)
	var lastHeldDesired int32
	var lastHighTime time.Time
	for i := range out.Snapshots {
		desired := out.Snapshots[i].Desired
		if desired < minReplicas {
			desired = minReplicas
		}
		if desired > maxReplicas {
			desired = maxReplicas
			out.Snapshots[i].Health = "LIMITED"
		}
		if candidate.ScaleDownStabilizationSeconds > 0 {
			if desired >= lastHeldDesired {
				lastHeldDesired = desired
				lastHighTime = out.Snapshots[i].Timestamp
			} else if !lastHighTime.IsZero() {
				window := time.Duration(candidate.ScaleDownStabilizationSeconds) * time.Second
				if out.Snapshots[i].Timestamp.Sub(lastHighTime) < window {
					desired = lastHeldDesired
				} else {
					lastHeldDesired = desired
					lastHighTime = out.Snapshots[i].Timestamp
				}
			}
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
	_, _ = fmt.Fprintf(out, "Scenario comparison: %s/%s\n", report.Namespace, report.Name)
	_, _ = fmt.Fprintf(out, "Replay Summary: %s / %s\n\n", report.Name, report.Namespace)
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
