package hpa

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// StatusReport holds the analysis result and events for a single HPA.
type StatusReport struct {
	Analysis Analysis `json:"analysis" yaml:"analysis"`
	Events   []Event  `json:"events,omitempty" yaml:"events,omitempty"`
}

// StatusTextOptions configures text output rendering with theme, language, fix mode, and diff display.
type StatusTextOptions struct {
	Theme         style.Theme
	Lang          string
	Fix           bool
	Diff          bool
	HiddenFactors bool
	Labels        LabelProvider // When nil, English defaults are used. Takes precedence over Lang.
}

// WatchState holds the previous and current Analysis for diff display.
type WatchState struct {
	Previous *Analysis
	Current  *Analysis
}

// WriteStatusText writes a plain text status report using the given theme.
func WriteStatusText(w io.Writer, report StatusReport, theme style.Theme) error {
	return WriteStatusTextWithOptions(w, report, StatusTextOptions{Theme: theme})
}

// WriteScalePathText writes only the scale path section.
func WriteScalePathText(w io.Writer, path *ScalePath) error {
	if path == nil {
		_, err := fmt.Fprintln(w, "Scale Path:\n  No scale path data available.")
		return err
	}
	var out []byte
	appendScalePathText(&out, path, style.Theme{})
	_, err := w.Write(out)
	return err
}

// WriteStatusDashboard writes a compact dashboard format status report.
func WriteStatusDashboard(w io.Writer, report StatusReport, theme style.Theme) error {
	a := report.Analysis
	diff := a.Desired - a.Current
	var out []byte
	out = fmt.Appendf(out, "kubectl-hpa-status dashboard\n")
	out = fmt.Appendf(out, "HPA      %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "Target   %s\n", a.Target)
	out = fmt.Appendf(out, "Health   %s %d/100\n", theme.HealthLabel(a.Health, a.HealthScore), a.HealthScore)
	out = fmt.Appendf(out, "Replicas current=%d desired=%d diff=%+d min=%d max=%d\n", a.Current, a.Desired, diff, a.Min, a.Max)
	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		progress := FormatStabilizationWithSource(a.StabilizationRemaining, a.StabilizationWindowSeconds, a.StabilizationSource)
		out = fmt.Appendf(out, "Stabilizing %s\n", progress)
	}
	out = fmt.Appendf(out, "Summary  %s\n", theme.SummaryColor(a.Summary))

	out = append(out, "\nConditions\n"...)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		for _, condition := range a.Conditions {
			out = fmt.Appendf(out, "  %-15s %s %-24s %s\n", condition.Type, theme.ConditionStatus(condition.Type, condition.Status), condition.Reason, condition.Message)
		}
	}

	out = append(out, "\nMetrics\n"...)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		for _, metric := range a.Metrics {
			name := metric.Name
			if name == "" {
				name = metric.Type
			}
			bar := "──────────"
			if metric.Ratio != nil {
				bar = progressBar(*metric.Ratio)
			}
			out = fmt.Appendf(out, "  %-24s %s current=%s target=%s %s\n", name, bar, metric.Current, metric.Target, theme.MetricNote(metric.Note))
		}
	}

	if len(a.Actions) > 0 {
		out = append(out, "\nNext actions\n"...)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	_, err := w.Write(out)
	return err
}

// triggerStatusBadge returns a display string with a visual indicator for a KEDA trigger status.
func triggerStatusBadge(status string, theme style.Theme) string {
	switch status {
	case "Active":
		return theme.OK.Render("Active ✓")
	case "Inactive":
		return theme.Error.Render("Inactive ✗")
	default:
		return theme.Dim.Render("Unknown ?")
	}
}

// metricsDiagnosticsStatus returns a themed display string for the overall metrics diagnostics status.
func metricsDiagnosticsStatus(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("healthy")
	case "degraded":
		return theme.Bold.Render("degraded")
	case "error":
		return theme.Error.Render("error")
	default:
		return theme.Dim.Render(status)
	}
}

// metricsDiagnosticsIndicator returns a themed status indicator for a per-metric health check.
func metricsDiagnosticsIndicator(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("✓")
	case "missing":
		return theme.Error.Render("✗")
	case "stale":
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// WriteStatusTextWithOptions writes a status report with full rendering options.
//
//nolint:gocyclo // Sequential text rendering of independent status sections; splitting would reduce readability.
func WriteStatusTextWithOptions(w io.Writer, report StatusReport, opts StatusTextOptions) error {
	a := report.Analysis
	theme := opts.Theme
	labels := resolveLabels(opts.Labels)
	var out []byte
	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "%s: %s\n", labels.Target, a.Target)

	// Replicas: highlight desired when it differs from current
	desired := theme.ReplicaHighlight(a.Desired, a.Desired != a.Current)
	out = fmt.Appendf(out, "%s: current=%d desired=%s min=%d max=%d\n", labels.Replicas, a.Current, desired, a.Min, a.Max)
	out = fmt.Appendf(out, "%s: %s %d/100\n", labels.Health, theme.HealthLabel(a.Health, a.HealthScore), a.HealthScore)
	if a.HealthResult != nil && len(a.HealthResult.Signals) > 0 {
		out = append(out, "Score Breakdown:\n"...)
		out = append(out, "  base: 100\n"...)
		for _, signal := range a.HealthResult.Signals {
			out = fmt.Appendf(out, "  -%d %s (%s)\n", signal.Penalty, signal.Reason, signal.Severity)
		}
		out = fmt.Appendf(out, "  final: %d\n", a.HealthScore)
	}

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s: %s\n", labels.Summary, theme.SummaryColor(a.Summary))

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s:\n", labels.Conditions)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		for _, condition := range a.Conditions {
			statusText := theme.ConditionStatus(condition.Type, condition.Status)
			out = fmt.Appendf(out, "  %-15s %-7s %-24s %s\n", condition.Type, statusText, condition.Reason, condition.Message)
		}
	}

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s:\n", labels.Metrics)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		for _, metric := range a.Metrics {
			note := theme.MetricNote(metric.Note)
			text := formatMetricText(metric, note)
			out = fmt.Appendf(out, "  - %s\n", text)
		}
	}

	// Stabilization window section.
	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		out = append(out, '\n')
		if a.StabilizationConfidence != "" {
			// Enhanced display with progress bar, source, and confidence.
			explain := FormatStabilizationExplain(a)
			out = fmt.Appendf(out, "%s\n", theme.Warning.Render(explain))
		} else {
			progress := FormatStabilizationProgress(a.StabilizationRemaining, a.StabilizationWindowSeconds)
			out = fmt.Appendf(out, "Stabilization: %s\n", theme.Warning.Render(progress))
		}
	}
	if len(a.Behavior) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Behavior)
		for _, behavior := range a.Behavior {
			out = fmt.Appendf(out, "  - %s\n", behavior.Text)
		}
	}

	if opts.HiddenFactors && len(a.HiddenFactors) > 0 {
		out = append(out, '\n')
		out = append(out, "Hidden decision factors:\n"...)
		for _, factor := range a.HiddenFactors {
			out = fmt.Appendf(out, "  - %s: %s\n", factor.Name, factor.Status)
			for _, evidence := range factor.Evidence {
				out = fmt.Appendf(out, "    evidence: %s\n", evidence)
			}
			out = fmt.Appendf(out, "    impact: %s\n", factor.Impact)
			out = fmt.Appendf(out, "    confidence: %s\n", factor.Confidence)
		}
	}

	if a.HealthTrend != nil && len(a.HealthTrend.Snapshots) > 0 {
		out = append(out, '\n')
		trendText := FormatTrendText(*a.HealthTrend)
		out = fmt.Appendf(out, "%s\n", trendText)
	}

	if a.ControllerProfile != nil {
		out = append(out, '\n')
		appendControllerProfileText(&out, a.ControllerProfile)
	}

	if len(a.Actions) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Actions)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	if len(a.Suggestions) > 0 {
		out = append(out, '\n')
		if opts.Fix {
			out = fmt.Appendf(out, "%s:\n", labels.Fix)
		} else {
			out = fmt.Appendf(out, "%s:\n", labels.Suggestions)
		}
		for _, suggestion := range a.Suggestions {
			out = fmt.Appendf(out, "  - %s: %s", suggestion.Title, suggestion.Description)
			if suggestion.Risk != "" {
				out = fmt.Appendf(out, " (%s: %s)", labels.Risk, suggestion.Risk)
			}
			out = append(out, '\n')
			if suggestion.Command != "" {
				out = fmt.Appendf(out, "    $ %s\n", theme.ActionLine(suggestion.Command))
			}
			if opts.Diff && suggestion.Patch != "" {
				currentMin := a.Min
				out = fmt.Appendf(out, "    diff:\n%s", indentBlock(SuggestionDiff(&currentMin, a.Desired, a.Max, suggestion.Patch), "      "))
			}
			for _, precondition := range suggestion.Preconditions {
				out = fmt.Appendf(out, "    %s: %s\n", labels.Precondition, precondition)
			}
			for _, warning := range suggestion.Warnings {
				out = fmt.Appendf(out, "    %s: %s\n", labels.Warning, theme.ActionLine(warning))
			}
		}
	}

	if len(a.Interpretation) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Interpretation)
		for _, line := range a.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}
	}

	if len(a.Debug) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.Debug)
		for _, line := range a.Debug {
			out = fmt.Appendf(out, "  - %s\n", theme.Dim.Render(line))
		}
	}

	if len(a.DecisionSignals) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s\n", FormatDecisionSignals(a.DecisionSignals))
	}

	if a.DecisionTrace != nil {
		out = append(out, '\n')
		AppendDecisionTraceText(&out, a.DecisionTrace)
	}

	if a.StructuredDecisionTrace != nil {
		out = append(out, '\n')
		AppendStructuredDecisionTraceText(&out, a.StructuredDecisionTrace, opts.Labels)
	}

	if a.AdapterDiagnostics != nil {
		out = append(out, '\n')
		AppendAdapterDiagnosticsText(&out, a.AdapterDiagnostics)
	}

	if a.KEDAInfo != nil {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.KEDA)
		out = fmt.Appendf(out, "  ScaledObject: %s\n", a.KEDAInfo.ScaledObjectName)
		if len(a.KEDAInfo.Triggers) > 0 {
			out = fmt.Appendf(out, "  Triggers:\n")
			for _, t := range a.KEDAInfo.Triggers {
				label := t.Type
				if t.Name != "" {
					label = fmt.Sprintf("%s (%s)", t.Type, t.Name)
				}
				if t.Status != "" {
					badge := triggerStatusBadge(t.Status, theme)
					label = fmt.Sprintf("%s: %s", label, badge)
				}
				out = fmt.Appendf(out, "    - %s\n", label)
				if t.MetricName != "" || t.Threshold != "" || t.CurrentValue != "" {
					detail := ""
					if t.MetricName != "" {
						detail = fmt.Sprintf("metric=%s", t.MetricName)
					}
					if t.Threshold != "" {
						if detail != "" {
							detail += " "
						}
						detail += fmt.Sprintf("threshold=%s", t.Threshold)
					}
					if t.CurrentValue != "" {
						if detail != "" {
							detail += " "
						}
						detail += fmt.Sprintf("current=%s", t.CurrentValue)
					}
					out = fmt.Appendf(out, "      %s\n", detail)
				}
				if t.AuthRef != "" {
					out = fmt.Appendf(out, "      authRef=%s\n", t.AuthRef)
				}
			}
		}
		if a.KEDAInfo.Fallback != nil {
			out = fmt.Appendf(out, "  Fallback: failureThreshold=%d, replicas=%d\n", a.KEDAInfo.Fallback.FailureThreshold, a.KEDAInfo.Fallback.Replicas)
		}
		if a.KEDAInfo.PollingInterval != nil {
			out = fmt.Appendf(out, "  Polling interval: %ds\n", *a.KEDAInfo.PollingInterval)
		}
		if a.KEDAInfo.CooldownPeriod != nil {
			out = fmt.Appendf(out, "  Cooldown period: %ds\n", *a.KEDAInfo.CooldownPeriod)
		}
		if a.KEDAInfo.MinReplicaCount != nil {
			out = fmt.Appendf(out, "  Min replica count: %d\n", *a.KEDAInfo.MinReplicaCount)
		}
		if a.KEDAInfo.MaxReplicaCount != nil {
			out = fmt.Appendf(out, "  Max replica count: %d\n", *a.KEDAInfo.MaxReplicaCount)
		}
		for _, line := range a.KEDAInfo.Lines {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}
	}

	if a.VPAConflict != nil {
		out = append(out, '\n')
		out = fmt.Appendf(out, "VPA:\n")
		out = fmt.Appendf(out, "  %s targets %s/%s", a.VPAConflict.VPAName, a.VPAConflict.TargetKind, a.VPAConflict.TargetName)
		if a.VPAConflict.UpdateMode != "" {
			out = fmt.Appendf(out, " updateMode=%s", a.VPAConflict.UpdateMode)
		}
		out = append(out, '\n')
		if len(a.VPAConflict.ControlledResources) > 0 {
			out = fmt.Appendf(out, "  Controlled resources: %s\n", strings.Join(a.VPAConflict.ControlledResources, ", "))
		}
		for _, rec := range a.VPAConflict.Recommendations {
			out = fmt.Appendf(out, "  - %s/%s target=%s lower=%s upper=%s\n", rec.Container, rec.Resource, emptyAsUnknown(rec.Target), emptyAsUnknown(rec.Lower), emptyAsUnknown(rec.Upper))
		}
		out = fmt.Appendf(out, "  warning: %s\n", theme.ActionLine(a.VPAConflict.Warning))
	}

	if a.MetricsDiagnostics != nil {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.MetricsDiagnostics)
		out = fmt.Appendf(out, "  %s: %s\n", labels.Health, metricsDiagnosticsStatus(a.MetricsDiagnostics.OverallStatus, theme))
		if len(a.MetricsDiagnostics.PerMetricChecks) > 0 {
			out = append(out, "  Checks:\n"...)
			for _, check := range a.MetricsDiagnostics.PerMetricChecks {
				indicator := metricsDiagnosticsIndicator(check.Status, theme)
				out = fmt.Appendf(out, "    - %s %s/%s: %s\n", indicator, check.MetricType, check.MetricName, check.Details)
				if check.Remediation != "" {
					out = fmt.Appendf(out, "      %s\n", check.Remediation)
				}
			}
		}
		if len(a.MetricsDiagnostics.RemediationSteps) > 0 {
			out = append(out, "  Remediation:\n"...)
			for _, step := range a.MetricsDiagnostics.RemediationSteps {
				out = fmt.Appendf(out, "    - %s\n", step)
			}
		}
	}

	if len(a.MetricFreshnessEntries) > 0 {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.MetricFreshness)
		for _, mf := range a.MetricFreshnessEntries {
			indicator := metricFreshnessIndicator(mf.Status, theme)
			out = fmt.Appendf(out, "  %s %s/%s:\n", indicator, mf.Name, strings.ToLower(mf.Type))
			out = fmt.Appendf(out, "    status: %s\n", metricFreshnessStatusDisplay(mf.Status, theme))
			if mf.LastSeen != nil {
				out = fmt.Appendf(out, "    last sample: %s ago\n", formatFreshnessDuration(mf.Age))
			}
			if mf.Source != "" {
				out = fmt.Appendf(out, "    source: %s\n", mf.Source)
			}
			if mf.APIServiceAvailable != nil {
				status := "unavailable"
				if *mf.APIServiceAvailable {
					status = "available"
				}
				out = fmt.Appendf(out, "    apiservice: %s", status)
				if mf.APIServiceMessage != "" {
					out = fmt.Appendf(out, " (%s)", mf.APIServiceMessage)
				}
				out = append(out, '\n')
			}
			if mf.Window != "" {
				out = fmt.Appendf(out, "    window: %s\n", mf.Window)
			}
			if mf.LastEvent != nil {
				out = fmt.Appendf(out, "    last HPA event: %s", mf.LastEvent.Reason)
				if !mf.LastEvent.Timestamp.IsZero() {
					out = fmt.Appendf(out, " %s ago", formatFreshnessDuration(now().Sub(mf.LastEvent.Timestamp)))
				}
				out = append(out, '\n')
			}
			if mf.Risk != "" {
				out = fmt.Appendf(out, "    likely cause: %s\n", theme.ActionLine(mf.Risk))
			}
			if len(mf.Evidence) > 0 {
				out = append(out, "    evidence:\n"...)
				for _, e := range mf.Evidence {
					out = fmt.Appendf(out, "      - %s\n", e)
				}
			}
			if len(mf.NextSteps) > 0 {
				out = append(out, "    next checks:\n"...)
				for _, ns := range mf.NextSteps {
					out = fmt.Appendf(out, "      %s\n", theme.ActionLine(ns))
				}
			}
		}
	}

	if a.ResourceCheck != nil && len(a.ResourceCheck.Warnings) > 0 {
		out = append(out, '\n')
		out = append(out, "Resource Check:\n"...)
		for _, w := range a.ResourceCheck.Warnings {
			indicator := "warning"
			if w.Severity == "error" {
				indicator = "error"
			}
			out = fmt.Appendf(out, "  [%s] %s/%s: %s\n", indicator, w.Container, w.Resource, w.Details)
		}
	}

	if a.PodAnalysis != nil {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s:\n", labels.PodAnalysis)
		pa := a.PodAnalysis
		out = fmt.Appendf(out, "  Total: %d  Ready: %d  Unready: %d  Pending: %d  Terminating: %d\n",
			pa.Total, pa.Ready, pa.Unready, pa.Pending, pa.Terminating)
		for _, issue := range pa.ResourceIssues {
			out = fmt.Appendf(out, "  [%s] %s/%s: %s\n", issue.Category, issue.Pod, issue.Container, issue.Resource)
		}
		for _, check := range pa.ContainerChecks {
			if !check.Found {
				out = fmt.Appendf(out, "  [warning] %s\n", check.Message)
			}
		}
	}

	if a.Simulation != nil {
		out = append(out, '\n')
		out = fmt.Appendf(out, "%s (--simulate):\n", labels.Simulation)
		sim := a.Simulation
		out = fmt.Appendf(out, "  Parameter: %s\n", sim.Parameter)
		out = fmt.Appendf(out, "  Original: %s  Simulated: %s\n", sim.OriginalValue, sim.SimulatedValue)
		out = fmt.Appendf(out, "  Before: desired=%d health=%s(%d)  After: desired=%d health=%s(%d)\n",
			sim.Before.DesiredReplicas, sim.Before.Health, sim.Before.HealthScore,
			sim.After.DesiredReplicas, sim.After.Health, sim.After.HealthScore)
		if sim.RiskAssessment != "" {
			out = fmt.Appendf(out, "  Risk: %s\n", theme.ActionLine(sim.RiskAssessment))
		}
		for _, line := range sim.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}

		// Extended simulation output: trajectory and risk warnings.
		if sim.TimeSeriesProjection != nil || len(sim.RiskWarnings) > 0 {
			extText := FormatSimulationExtended(sim)
			if extText != "" {
				out = fmt.Appendf(out, "%s\n", extText)
			}
		}
	}

	if a.CapacityContext != nil {
		cc := a.CapacityContext
		if len(cc.PendingPods) > 0 || len(cc.QuotaConstraints) > 0 || len(cc.PDBInterference) > 0 || len(cc.NodeHints) > 0 {
			out = append(out, '\n')
			out = fmt.Appendf(out, "%s:\n", labels.CapacityContext)
			if len(cc.PendingPods) > 0 {
				out = fmt.Appendf(out, "  Pending Pods: %d\n", len(cc.PendingPods))
				for _, p := range cc.PendingPods {
					suffix := ""
					if p.Unschedulable {
						suffix = " (unschedulable)"
					}
					out = fmt.Appendf(out, "    - %s%s\n", p.Name, suffix)
					for _, reason := range p.Reasons {
						out = fmt.Appendf(out, "      %s\n", reason)
					}
				}
			}
			if len(cc.QuotaConstraints) > 0 {
				out = append(out, "  ResourceQuotas:\n"...)
				for _, q := range cc.QuotaConstraints {
					out = fmt.Appendf(out, "    - %s: %s used=%s hard=%s\n", q.Name, q.Resource, q.Used, q.Hard)
				}
			}
			if len(cc.PDBInterference) > 0 {
				out = append(out, "  PodDisruptionBudgets:\n"...)
				for _, p := range cc.PDBInterference {
					out = fmt.Appendf(out, "    - %s: %s\n", p.Name, p.Disruption)
				}
			}
			if len(cc.NodeHints) > 0 {
				out = append(out, "  Hints:\n"...)
				for _, hint := range cc.NodeHints {
					out = fmt.Appendf(out, "    - %s\n", theme.InterpretationLine(hint))
				}
			}
		}
	}

	if a.CapacityHeadroom != nil {
		out = append(out, '\n')
		appendCapacityHeadroomText(&out, a.CapacityHeadroom, theme)
	}

	if a.ReadinessImpact != nil {
		out = append(out, '\n')
		appendReadinessImpactText(&out, a.ReadinessImpact, theme)
	}

	if a.ScalePath != nil {
		out = append(out, '\n')
		appendScalePathText(&out, a.ScalePath, theme)
	}

	if a.RolloutDiagnosis != nil {
		out = append(out, '\n')
		appendRolloutDiagnosisText(&out, a.RolloutDiagnosis, theme)
	}

	if a.BlockerReport != nil {
		AppendBlockerText(&out, a.BlockerReport, theme, labels)
		appendScaleoutBlockersText(&out, a.BlockerReport, theme)
	}

	if a.CapacityPlan != nil {
		AppendCapacityPlanText(&out, a.CapacityPlan, theme, labels)
	}

	if a.MetricContract != nil {
		out = append(out, '\n')
		appendMetricContractText(&out, a.MetricContract, theme)
	}

	if a.ContainerAdvisor != nil {
		AppendContainerAdvisorText(&out, a.ContainerAdvisor, labels)
	}

	if a.BehaviorAdvisor != nil {
		AppendBehaviorAdvisorText(&out, a.BehaviorAdvisor, labels)
	}

	if a.FlappingPrevention != nil {
		AppendFlappingPreventionText(&out, a.FlappingPrevention, labels)
	}

	if a.MetricHints != nil && len(a.MetricHints.TroubleshootingFlows) > 0 {
		out = append(out, '\n')
		out = append(out, "Metric Troubleshooting:\n"...)
		for _, flow := range a.MetricHints.TroubleshootingFlows {
			out = fmt.Appendf(out, "  [%s] %s (%s/%s)\n", flow.Severity, flow.Title, flow.MetricType, flow.MetricName)
			for _, step := range flow.Steps {
				out = fmt.Appendf(out, "    %d. %s\n", step.StepNumber, step.Description)
				if step.Command != "" {
					out = fmt.Appendf(out, "       $ %s\n", step.Command)
				}
			}
		}
	}

	out = append(out, '\n')
	out = fmt.Appendf(out, "%s:\n", labels.Events)
	if len(report.Events) == 0 {
		out = append(out, "  No recent events found.\n"...)
	} else {
		for _, event := range report.Events {
			out = fmt.Appendf(out, "  - %s: %s\n", event.Reason, event.Message)
		}
	}

	_, err := w.Write(out)
	return err
}

func emptyAsUnknown(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
}

func indentBlock(text string, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}

type labels struct {
	Target              string
	Replicas            string
	Health              string
	Summary             string
	Conditions          string
	Metrics             string
	Behavior            string
	Actions             string
	Suggestions         string
	Fix                 string
	Interpretation      string
	Debug               string
	KEDA                string
	Events              string
	Risk                string
	Precondition        string
	Warning             string
	MetricsDiagnostics  string
	MetricFreshness     string
	PodAnalysis         string
	Simulation          string
	CapacityContext     string
	Timeline            string
	MetricDecisionTrace string
	AuditFindings       string
	AuditScore          string
	AuditSeverity       string
	Blockers            string
	NextCommands        string
	CapacityPlan        string
	MetricContract      string
	Warmup              string
	ContainerAdvisor    string
	BehaviorAdvisor     string
	FlappingPrevention  string
}

// metricFreshnessIndicator returns a themed status indicator for a metric
// freshness entry.
func metricFreshnessIndicator(status string, theme style.Theme) string {
	switch status {
	case string(FreshnessOK):
		return theme.OK.Render("✓")
	case string(FreshnessMissing):
		return theme.Error.Render("✗")
	case string(FreshnessStale):
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// metricFreshnessStatusDisplay returns a themed display string for the
// freshness status label.
func metricFreshnessStatusDisplay(status string, theme style.Theme) string {
	switch status {
	case string(FreshnessOK):
		return theme.OK.Render("OK")
	case string(FreshnessMissing):
		return theme.Error.Render("MISSING")
	case string(FreshnessStale):
		return theme.Bold.Render("STALE")
	default:
		return theme.Dim.Render("UNKNOWN")
	}
}

// formatFreshnessDuration returns a human-readable duration string
// (e.g., "12s", "4m32s", "1h5m").
func formatFreshnessDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int64(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	hours := minutes / 60
	mins := minutes % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}
