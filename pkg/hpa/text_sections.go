package hpa

import (
	"fmt"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// This file holds the per-section renderers invoked by WriteStatusTextWithOptions.
// Each append*Section function appends its rendering to *out, guarded by a nil
// check so the caller can list them unconditionally. Splitting the sections out
// keeps the main orchestrator readable and lets each section be tested in
// isolation.

// appendScoreBreakdown renders the optional score breakdown table.
func appendScoreBreakdown(out *[]byte, a Analysis) {
	if a.HealthResult == nil || len(a.HealthResult.Signals) == 0 {
		return
	}
	*out = append(*out, "Score Breakdown:\n"...)
	*out = append(*out, "  base: 100\n"...)
	for _, signal := range a.HealthResult.Signals {
		*out = fmt.Appendf(*out, "  -%d %s (%s)\n", signal.Penalty, signal.Reason, signal.Severity)
	}
	*out = fmt.Appendf(*out, "  final: %d\n", a.HealthScore)
}

// appendConditionsSection renders the Conditions section.
func appendConditionsSection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Conditions)
	if len(a.Conditions) == 0 {
		*out = append(*out, "  No conditions reported.\n"...)
		return
	}
	for _, condition := range a.Conditions {
		statusText := theme.ConditionStatus(condition.Type, condition.Status)
		*out = fmt.Appendf(*out, "  %-15s %-7s %-24s %s\n", condition.Type, statusText, condition.Reason, condition.Message)
	}
}

// appendMetricsSection renders the Metrics section.
func appendMetricsSection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Metrics)
	if len(a.Metrics) == 0 {
		*out = append(*out, "  No current metrics reported.\n"...)
		return
	}
	for _, metric := range a.Metrics {
		note := theme.MetricNote(metric.Note)
		text := formatMetricText(metric, note)
		*out = fmt.Appendf(*out, "  - %s\n", text)
	}
}

// appendStabilizationAndBehavior renders the stabilization window and behavior sections.
func appendStabilizationAndBehavior(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
		*out = append(*out, '\n')
		if a.StabilizationConfidence != "" {
			explain := FormatStabilizationExplain(a)
			*out = fmt.Appendf(*out, "%s\n", theme.Warning.Render(explain))
		} else {
			progress := FormatStabilizationProgress(a.StabilizationRemaining, a.StabilizationWindowSeconds)
			*out = fmt.Appendf(*out, "Stabilization: %s\n", theme.Warning.Render(progress))
		}
	}
	if len(a.Behavior) > 0 {
		*out = append(*out, '\n')
		*out = fmt.Appendf(*out, "%s:\n", labels.Behavior)
		for _, behavior := range a.Behavior {
			*out = fmt.Appendf(*out, "  - %s\n", behavior.Text)
		}
	}
}

// appendHiddenFactors renders the hidden decision factors section when requested.
func appendHiddenFactors(out *[]byte, a Analysis, opts StatusTextOptions) {
	if !opts.HiddenFactors || len(a.HiddenFactors) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Hidden decision factors:\n"...)
	for _, factor := range a.HiddenFactors {
		*out = fmt.Appendf(*out, "  - %s: %s\n", factor.Name, factor.Status)
		for _, evidence := range factor.Evidence {
			*out = fmt.Appendf(*out, "    evidence: %s\n", evidence)
		}
		*out = fmt.Appendf(*out, "    impact: %s\n", factor.Impact)
		*out = fmt.Appendf(*out, "    confidence: %s\n", factor.Confidence)
	}
}

// appendSuggestionsSection renders the suggestions (or fix-mode) section.
func appendSuggestionsSection(out *[]byte, a Analysis, opts StatusTextOptions, theme style.Theme, labels labels) {
	if len(a.Suggestions) == 0 {
		return
	}
	*out = append(*out, '\n')
	if opts.Fix {
		*out = fmt.Appendf(*out, "%s:\n", labels.Fix)
	} else {
		*out = fmt.Appendf(*out, "%s:\n", labels.Suggestions)
	}
	for _, suggestion := range a.Suggestions {
		*out = fmt.Appendf(*out, "  - %s: %s", suggestion.Title, suggestion.Description)
		if suggestion.Risk != "" {
			*out = fmt.Appendf(*out, " (%s: %s)", labels.Risk, suggestion.Risk)
		}
		*out = append(*out, '\n')
		if suggestion.Command != "" {
			*out = fmt.Appendf(*out, "    $ %s\n", theme.ActionLine(suggestion.Command))
		}
		if opts.Diff && suggestion.Patch != "" {
			currentMin := a.Min
			*out = fmt.Appendf(*out, "    diff:\n%s", indentBlock(SuggestionDiff(&currentMin, a.Desired, a.Max, suggestion.Patch), "      "))
		}
		for _, precondition := range suggestion.Preconditions {
			*out = fmt.Appendf(*out, "    %s: %s\n", labels.Precondition, precondition)
		}
		for _, warning := range suggestion.Warnings {
			*out = fmt.Appendf(*out, "    %s: %s\n", labels.Warning, theme.ActionLine(warning))
		}
	}
}

// appendKEDASection renders the KEDA ScaledObject section.
//
//nolint:gocyclo // Flat, sequential rendering of independent KEDA fields; splitting would obscure the output shape.
func appendKEDASection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	if a.KEDAInfo == nil {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.KEDA)
	*out = fmt.Appendf(*out, "  ScaledObject: %s\n", a.KEDAInfo.ScaledObjectName)
	if len(a.KEDAInfo.Triggers) > 0 {
		*out = fmt.Appendf(*out, "  Triggers:\n")
		for _, t := range a.KEDAInfo.Triggers {
			label := t.Type
			if t.Name != "" {
				label = fmt.Sprintf("%s (%s)", t.Type, t.Name)
			}
			if t.Status != "" {
				badge := triggerStatusBadge(t.Status, theme)
				label = fmt.Sprintf("%s: %s", label, badge)
			}
			*out = fmt.Appendf(*out, "    - %s\n", label)
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
				*out = fmt.Appendf(*out, "      %s\n", detail)
			}
			if t.AuthRef != "" {
				*out = fmt.Appendf(*out, "      authRef=%s\n", t.AuthRef)
			}
		}
	}
	if a.KEDAInfo.Fallback != nil {
		*out = fmt.Appendf(*out, "  Fallback: failureThreshold=%d, replicas=%d\n", a.KEDAInfo.Fallback.FailureThreshold, a.KEDAInfo.Fallback.Replicas)
	}
	if a.KEDAInfo.PollingInterval != nil {
		*out = fmt.Appendf(*out, "  Polling interval: %ds\n", *a.KEDAInfo.PollingInterval)
	}
	if a.KEDAInfo.CooldownPeriod != nil {
		*out = fmt.Appendf(*out, "  Cooldown period: %ds\n", *a.KEDAInfo.CooldownPeriod)
	}
	if a.KEDAInfo.MinReplicaCount != nil {
		*out = fmt.Appendf(*out, "  Min replica count: %d\n", *a.KEDAInfo.MinReplicaCount)
	}
	if a.KEDAInfo.MaxReplicaCount != nil {
		*out = fmt.Appendf(*out, "  Max replica count: %d\n", *a.KEDAInfo.MaxReplicaCount)
	}
	for _, line := range a.KEDAInfo.Lines {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(line))
	}
}

// appendVPASection renders the VPA conflict section.
func appendVPASection(out *[]byte, a Analysis, theme style.Theme) {
	if a.VPAConflict == nil {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "VPA:\n")
	*out = fmt.Appendf(*out, "  %s targets %s/%s", a.VPAConflict.VPAName, a.VPAConflict.TargetKind, a.VPAConflict.TargetName)
	if a.VPAConflict.UpdateMode != "" {
		*out = fmt.Appendf(*out, " updateMode=%s", a.VPAConflict.UpdateMode)
	}
	*out = append(*out, '\n')
	if len(a.VPAConflict.ControlledResources) > 0 {
		*out = fmt.Appendf(*out, "  Controlled resources: %s\n", strings.Join(a.VPAConflict.ControlledResources, ", "))
	}
	for _, rec := range a.VPAConflict.Recommendations {
		*out = fmt.Appendf(*out, "  - %s/%s target=%s lower=%s upper=%s\n", rec.Container, rec.Resource, emptyAsUnknown(rec.Target), emptyAsUnknown(rec.Lower), emptyAsUnknown(rec.Upper))
	}
	*out = fmt.Appendf(*out, "  warning: %s\n", theme.ActionLine(a.VPAConflict.Warning))
}

// appendMetricsDiagnosticsSection renders the metrics pipeline diagnostics section.
func appendMetricsDiagnosticsSection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	if a.MetricsDiagnostics == nil {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.MetricsDiagnostics)
	*out = fmt.Appendf(*out, "  %s: %s\n", labels.Health, metricsDiagnosticsStatus(a.MetricsDiagnostics.OverallStatus, theme))
	if len(a.MetricsDiagnostics.PerMetricChecks) > 0 {
		*out = append(*out, "  Checks:\n"...)
		for _, check := range a.MetricsDiagnostics.PerMetricChecks {
			indicator := metricsDiagnosticsIndicator(check.Status, theme)
			*out = fmt.Appendf(*out, "    - %s %s/%s: %s\n", indicator, check.MetricType, check.MetricName, check.Details)
			if check.Remediation != "" {
				*out = fmt.Appendf(*out, "      %s\n", check.Remediation)
			}
		}
	}
	if len(a.MetricsDiagnostics.RemediationSteps) > 0 {
		*out = append(*out, "  Remediation:\n"...)
		for _, step := range a.MetricsDiagnostics.RemediationSteps {
			*out = fmt.Appendf(*out, "    - %s\n", step)
		}
	}
}

// appendMetricFreshnessSection renders the metric freshness section.
//
//nolint:gocyclo // Flat, sequential rendering of independent freshness fields; splitting would obscure the output shape.
func appendMetricFreshnessSection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	if len(a.MetricFreshnessEntries) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.MetricFreshness)
	for _, mf := range a.MetricFreshnessEntries {
		indicator := metricFreshnessIndicator(mf.Status, theme)
		*out = fmt.Appendf(*out, "  %s %s/%s:\n", indicator, mf.Name, strings.ToLower(mf.Type))
		*out = fmt.Appendf(*out, "    status: %s\n", metricFreshnessStatusDisplay(mf.Status, theme))
		if mf.LastSeen != nil {
			*out = fmt.Appendf(*out, "    last sample: %s ago\n", formatFreshnessDuration(mf.Age))
		}
		if mf.Source != "" {
			*out = fmt.Appendf(*out, "    source: %s\n", mf.Source)
		}
		if mf.APIServiceAvailable != nil {
			status := "unavailable"
			if *mf.APIServiceAvailable {
				status = "available"
			}
			*out = fmt.Appendf(*out, "    apiservice: %s", status)
			if mf.APIServiceMessage != "" {
				*out = fmt.Appendf(*out, " (%s)", mf.APIServiceMessage)
			}
			*out = append(*out, '\n')
		}
		if mf.Window != "" {
			*out = fmt.Appendf(*out, "    window: %s\n", mf.Window)
		}
		if mf.LastEvent != nil {
			*out = fmt.Appendf(*out, "    last HPA event: %s", mf.LastEvent.Reason)
			if !mf.LastEvent.Timestamp.IsZero() {
				*out = fmt.Appendf(*out, " %s ago", formatFreshnessDuration(now().Sub(mf.LastEvent.Timestamp)))
			}
			*out = append(*out, '\n')
		}
		if mf.Risk != "" {
			*out = fmt.Appendf(*out, "    likely cause: %s\n", theme.ActionLine(mf.Risk))
		}
		if len(mf.Evidence) > 0 {
			*out = append(*out, "    evidence:\n"...)
			for _, e := range mf.Evidence {
				*out = fmt.Appendf(*out, "      - %s\n", e)
			}
		}
		if len(mf.NextSteps) > 0 {
			*out = append(*out, "    next checks:\n"...)
			for _, ns := range mf.NextSteps {
				*out = fmt.Appendf(*out, "      %s\n", theme.ActionLine(ns))
			}
		}
	}
}

// appendResourceCheckSection renders the resource-consistency warnings section.
func appendResourceCheckSection(out *[]byte, a Analysis) {
	if a.ResourceCheck == nil || len(a.ResourceCheck.Warnings) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Resource Check:\n"...)
	for _, w := range a.ResourceCheck.Warnings {
		indicator := "warning"
		if w.Severity == "error" {
			indicator = "error"
		}
		*out = fmt.Appendf(*out, "  [%s] %s/%s: %s\n", indicator, w.Container, w.Resource, w.Details)
	}
}

// appendPodAnalysisSection renders the pod-level analysis section.
func appendPodAnalysisSection(out *[]byte, a Analysis, labels labels) {
	if a.PodAnalysis == nil {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.PodAnalysis)
	pa := a.PodAnalysis
	*out = fmt.Appendf(*out, "  Total: %d  Ready: %d  Unready: %d  Pending: %d  Terminating: %d\n",
		pa.Total, pa.Ready, pa.Unready, pa.Pending, pa.Terminating)
	for _, issue := range pa.ResourceIssues {
		*out = fmt.Appendf(*out, "  [%s] %s/%s: %s\n", issue.Category, issue.Pod, issue.Container, issue.Resource)
	}
	for _, check := range pa.ContainerChecks {
		if !check.Found {
			*out = fmt.Appendf(*out, "  [warning] %s\n", check.Message)
		}
	}
}

// appendSimulationSection renders the --simulate output section.
func appendSimulationSection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	if a.Simulation == nil {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s (--simulate):\n", labels.Simulation)
	sim := a.Simulation
	*out = fmt.Appendf(*out, "  Parameter: %s\n", sim.Parameter)
	*out = fmt.Appendf(*out, "  Original: %s  Simulated: %s\n", sim.OriginalValue, sim.SimulatedValue)
	*out = fmt.Appendf(*out, "  Before: desired=%d health=%s(%d)  After: desired=%d health=%s(%d)\n",
		sim.Before.DesiredReplicas, sim.Before.Health, sim.Before.HealthScore,
		sim.After.DesiredReplicas, sim.After.Health, sim.After.HealthScore)
	if sim.RiskAssessment != "" {
		*out = fmt.Appendf(*out, "  Risk: %s\n", theme.ActionLine(sim.RiskAssessment))
	}
	for _, line := range sim.Interpretation {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(line))
	}
	// Extended simulation output: trajectory and risk warnings.
	if sim.TimeSeriesProjection != nil || len(sim.RiskWarnings) > 0 {
		extText := FormatSimulationExtended(sim)
		if extText != "" {
			*out = fmt.Appendf(*out, "%s\n", extText)
		}
	}
}

// appendCapacityContextSection renders the capacity-context section.
//
//nolint:gocyclo // Flat, sequential rendering of independent capacity fields; splitting would obscure the output shape.
func appendCapacityContextSection(out *[]byte, a Analysis, theme style.Theme, labels labels) {
	if a.CapacityContext == nil {
		return
	}
	cc := a.CapacityContext
	if len(cc.PendingPods) == 0 && len(cc.QuotaConstraints) == 0 && len(cc.PDBInterference) == 0 && len(cc.NodeHints) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.CapacityContext)
	if len(cc.PendingPods) > 0 {
		*out = fmt.Appendf(*out, "  Pending Pods: %d\n", len(cc.PendingPods))
		for _, p := range cc.PendingPods {
			suffix := ""
			if p.Unschedulable {
				suffix = " (unschedulable)"
			}
			*out = fmt.Appendf(*out, "    - %s%s\n", p.Name, suffix)
			for _, reason := range p.Reasons {
				*out = fmt.Appendf(*out, "      %s\n", reason)
			}
		}
	}
	if len(cc.QuotaConstraints) > 0 {
		*out = append(*out, "  ResourceQuotas:\n"...)
		for _, q := range cc.QuotaConstraints {
			*out = fmt.Appendf(*out, "    - %s: %s used=%s hard=%s\n", q.Name, q.Resource, q.Used, q.Hard)
		}
	}
	if len(cc.PDBInterference) > 0 {
		*out = append(*out, "  PodDisruptionBudgets:\n"...)
		for _, p := range cc.PDBInterference {
			*out = fmt.Appendf(*out, "    - %s: %s\n", p.Name, p.Disruption)
		}
	}
	if len(cc.NodeHints) > 0 {
		*out = append(*out, "  Hints:\n"...)
		for _, hint := range cc.NodeHints {
			*out = fmt.Appendf(*out, "    - %s\n", theme.InterpretationLine(hint))
		}
	}
}

// appendEventsSection renders the trailing events section.
func appendEventsSection(out *[]byte, report StatusReport, labels labels) {
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Events)
	if len(report.Events) == 0 {
		*out = append(*out, "  No recent events found.\n"...)
		return
	}
	for _, event := range report.Events {
		*out = fmt.Appendf(*out, "  - %s: %s\n", event.Reason, event.Message)
	}
}
