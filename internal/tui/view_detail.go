package tui

import (
	"fmt"
	"strings"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// renderDetailView renders the full detail view for the currently selected HPA.
//
// The body is intentionally split into one helper per section so that each
// block stays shallow (see the historical nestif=62 on the monolithic version).
// Sections are independent: a missing/empty field simply skips its block.
func (m Model) renderDetailView() string {
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return "No HPA selected."
	}

	item := filtered[m.cursor]
	key := item.Namespace + "/" + item.Name

	var sb strings.Builder
	renderDetailHeader(&sb, item)
	renderDetailHealth(&sb, item)
	renderDetailReplicas(&sb, item)
	renderDetailSummary(&sb, item)

	report, ok := m.reports[key]
	if !ok || report == nil {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("Press esc to go back, r to refresh, m metrics detail"))
		return sb.String()
	}

	a := report.Analysis
	renderDetailScoreBreakdown(&sb, report)
	renderDetailHiddenFactors(&sb, &a)
	renderDetailStabilization(&sb, &a)
	renderDetailConditions(&sb, &a)
	renderDetailMetrics(&sb, &a)
	renderDetailActions(&sb, &a)
	renderDetailInterpretation(&sb, &a)
	renderDetailDecisionSignals(&sb, &a)
	renderDetailTargetReplicas(&sb, &a)
	renderDetailKEDA(&sb, &a)
	renderDetailSuggestions(&sb, &a)
	renderDetailVPA(&sb, &a)

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press esc to go back, r to refresh, m metrics detail"))
	return sb.String()
}

func renderDetailHeader(sb *strings.Builder, item hpaanalysis.ListItem) {
	sb.WriteString(headerStyle.Render(fmt.Sprintf("HPA %s/%s", item.Namespace, item.Name)))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("Target: %s", item.Target)))
	sb.WriteString("\n\n")
}

func renderDetailHealth(sb *strings.Builder, item hpaanalysis.ListItem) {
	healthLabel := healthStyle(item.Health).Render(item.Health)
	scoreBar := renderScoreBar(item.HealthScore)
	sb.WriteString(fmt.Sprintf("Health: %s  %s  %d/100", healthLabel, scoreBar, item.HealthScore))
	sb.WriteString("\n\n")
}

func renderDetailReplicas(sb *strings.Builder, item hpaanalysis.ListItem) {
	diff := item.Desired - item.Current
	diffStr := fmt.Sprintf("%+d", diff)
	sb.WriteString(fmt.Sprintf("Replicas: current=%d  desired=%d  diff=%s  min=%d  max=%d",
		item.Current, item.Desired, diffStr, item.Min, item.Max))
	sb.WriteString("\n\n")
}

func renderDetailSummary(sb *strings.Builder, item hpaanalysis.ListItem) {
	sb.WriteString("Summary: ")
	sb.WriteString(item.Summary)
	sb.WriteString("\n")
}

func renderDetailScoreBreakdown(sb *strings.Builder, report *hpaanalysis.StatusReport) {
	if report.Analysis.Decision.HealthResult == nil || len(report.Analysis.Decision.HealthResult.Signals) == 0 {
		return
	}
	sb.WriteString("Score Breakdown:\n")
	sb.WriteString("  base: 100\n")
	for _, signal := range report.Analysis.Decision.HealthResult.Signals {
		sb.WriteString(fmt.Sprintf("  -%d %s (%s)\n", signal.Penalty, signal.Reason, signal.Severity))
	}
	sb.WriteString(fmt.Sprintf("  final: %d\n\n", report.Analysis.Decision.HealthScore))
}

func renderDetailHiddenFactors(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Lifecycle.HiddenFactors) == 0 {
		return
	}
	sb.WriteString("\nHidden decision factors:\n")
	for _, factor := range a.Lifecycle.HiddenFactors {
		sb.WriteString(fmt.Sprintf("  - %s: %s (%s)\n", factor.Name, factor.Status, factor.Confidence))
		if factor.Impact != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(factor.Impact)))
		}
	}
}

func renderDetailStabilization(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if a.Conditions.StabilizationRemaining == nil || *a.Conditions.StabilizationRemaining <= 0 {
		return
	}
	source := a.Conditions.StabilizationSource
	if source == "" {
		source = "scaleDown"
	}
	progress := hpaanalysis.FormatStabilizationProgress(a.Conditions.StabilizationRemaining, a.Conditions.StabilizationWindowSeconds)
	sb.WriteString("\n")
	sb.WriteString(warnStyle.Render(fmt.Sprintf("Stabilized (%s): %s", source, progress)))
	sb.WriteString("\n")
	if a.Conditions.StabilizationWindowSeconds != nil && *a.Conditions.StabilizationWindowSeconds > 0 {
		bar := renderEnhancedCountdownBar(int(*a.Conditions.StabilizationRemaining), int(*a.Conditions.StabilizationWindowSeconds))
		sb.WriteString(bar)
		sb.WriteString("\n")
	}
	sb.WriteString(dimStyle.Render("  [estimated]"))
	sb.WriteString("\n")
}

func renderDetailConditions(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Conditions.Conditions) == 0 {
		return
	}
	sb.WriteString("\nConditions:\n")
	for _, c := range a.Conditions.Conditions {
		statusStyle := okStyle
		if c.Status != "True" {
			statusStyle = errorStyle
		}
		sb.WriteString(fmt.Sprintf("  %-15s %s %s\n",
			hpaanalysis.SanitizeTerminalText(string(c.Type)),
			statusStyle.Render(hpaanalysis.SanitizeTerminalText(string(c.Status))),
			dimStyle.Render(hpaanalysis.SanitizeTerminalText(c.Reason)),
		))
	}
}

func renderDetailMetrics(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Metrics.Metrics) == 0 {
		return
	}
	sb.WriteString("\nMetrics:\n")
	for _, metric := range a.Metrics.Metrics {
		name := metric.Name
		if name == "" {
			name = metric.Type
		}
		ratio := ""
		if metric.Ratio != nil {
			ratio = fmt.Sprintf("  ratio=%.3f", *metric.Ratio)
		}
		sb.WriteString(fmt.Sprintf("  %-20s current=%s target=%s%s\n",
			name, metric.Current, metric.Target, dimStyle.Render(ratio),
		))
	}
}

func renderDetailActions(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Actions.Actions) == 0 {
		return
	}
	sb.WriteString("\nActions:\n")
	for _, action := range a.Actions.Actions {
		sb.WriteString(fmt.Sprintf("  • %s\n", action))
	}
}

func renderDetailInterpretation(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Actions.Interpretation) == 0 {
		return
	}
	sb.WriteString("\nInterpretation:\n")
	maxLines := 5
	for i, line := range a.Actions.Interpretation {
		if i >= maxLines {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(a.Actions.Interpretation)-maxLines)))
			break
		}
		sb.WriteString(dimStyle.Render("  " + line + "\n"))
	}
}

func renderDetailDecisionSignals(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Decision.DecisionSignals) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString(warnStyle.Render("Decision Signals:") + "\n")
	for _, sig := range a.Decision.DecisionSignals {
		confidence := sig.Confidence
		if confidence == "" {
			confidence = "unknown"
		}
		metricPart := ""
		if sig.MetricName != "" {
			metricPart = fmt.Sprintf(" metric=%s", sig.MetricName)
		}
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  - %s: %s%s [%s]\n",
			hpaanalysis.SanitizeTerminalText(sig.Reason), hpaanalysis.SanitizeTerminalText(sig.Message),
			hpaanalysis.SanitizeTerminalText(metricPart), hpaanalysis.SanitizeTerminalText(string(confidence)))))
	}
}

func renderDetailTargetReplicas(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if a.Replicas.TargetReplicas == nil {
		return
	}
	if a.Replicas.TargetReplicas.NotReady > 0 {
		sb.WriteString("\n")
		sb.WriteString(warnStyle.Render(fmt.Sprintf("%d of %d pods not ready", a.Replicas.TargetReplicas.NotReady, a.Replicas.TargetReplicas.TotalReplicas)))
		sb.WriteString("\n")
	}
	if a.Replicas.TargetReplicas.Pending > 0 {
		sb.WriteString("\n")
		sb.WriteString(warnStyle.Render(fmt.Sprintf("%d pods pending (%d unschedulable)", a.Replicas.TargetReplicas.Pending, a.Replicas.TargetReplicas.Unschedulable)))
		sb.WriteString("\n")
	}
}

// renderDetailKEDA renders the KEDA ScaledObject summary, including per-trigger
// details (metric/threshold/current/authRef). Extracted from renderDetailView to
// keep trigger-detail nesting shallow.
func renderDetailKEDA(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if a.Controllers.KEDAInfo == nil {
		return
	}
	sb.WriteString("\nKEDA:\n")
	sb.WriteString(fmt.Sprintf("  ScaledObject: %s\n", hpaanalysis.SanitizeTerminalText(a.Controllers.KEDAInfo.ScaledObjectName)))
	if len(a.Controllers.KEDAInfo.Triggers) > 0 {
		sb.WriteString("  Triggers:\n")
		for _, t := range a.Controllers.KEDAInfo.Triggers {
			renderDetailKEDATrigger(sb, t)
		}
	}
	if a.Controllers.KEDAInfo.PollingInterval != nil {
		sb.WriteString(fmt.Sprintf("  Polling interval: %ds\n", *a.Controllers.KEDAInfo.PollingInterval))
	}
	if a.Controllers.KEDAInfo.CooldownPeriod != nil {
		sb.WriteString(fmt.Sprintf("  Cooldown period: %ds\n", *a.Controllers.KEDAInfo.CooldownPeriod))
	}
	if a.Controllers.KEDAInfo.Fallback != nil {
		sb.WriteString(fmt.Sprintf("  Fallback: failureThreshold=%d, replicas=%d\n", a.Controllers.KEDAInfo.Fallback.FailureThreshold, a.Controllers.KEDAInfo.Fallback.Replicas))
	}
}

func renderDetailKEDATrigger(sb *strings.Builder, t hpakeda.TriggerSummary) {
	label := hpaanalysis.SanitizeTerminalText(t.Type)
	if t.Name != "" {
		label = fmt.Sprintf("%s (%s)", hpaanalysis.SanitizeTerminalText(t.Type), hpaanalysis.SanitizeTerminalText(t.Name))
	}
	if t.Status != "" {
		badge := tuiTriggerStatusBadge(t.Status)
		label = fmt.Sprintf("%s: %s", label, badge)
	}
	sb.WriteString(fmt.Sprintf("    - %s\n", label))
	if t.MetricName != "" || t.Threshold != "" || t.CurrentValue != "" {
		var detailParts []string
		if t.MetricName != "" {
			detailParts = append(detailParts, fmt.Sprintf("metric=%s", hpaanalysis.SanitizeTerminalText(t.MetricName)))
		}
		if t.Threshold != "" {
			detailParts = append(detailParts, fmt.Sprintf("threshold=%s", hpaanalysis.SanitizeTerminalText(t.Threshold)))
		}
		if t.CurrentValue != "" {
			detailParts = append(detailParts, fmt.Sprintf("current=%s", hpaanalysis.SanitizeTerminalText(t.CurrentValue)))
		}
		sb.WriteString(fmt.Sprintf("      %s\n", strings.Join(detailParts, " ")))
	}
	if t.AuthRef != "" {
		sb.WriteString(fmt.Sprintf("      authRef=%s\n", t.AuthRef))
	}
}

func renderDetailSuggestions(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if len(a.Actions.Suggestions) == 0 {
		return
	}
	sb.WriteString("\nSuggestions:\n")
	for _, suggestion := range a.Actions.Suggestions {
		sb.WriteString(fmt.Sprintf("  - %s (%s)\n", suggestion.Title, suggestion.Risk))
	}
	sb.WriteString(dimStyle.Render("  Use --fix --apply for the selected HPA to validate patches.\n"))
}

func renderDetailVPA(sb *strings.Builder, a *hpaanalysis.Analysis) {
	if a.Advisory.VPAConflict == nil {
		return
	}
	sb.WriteString("\nVPA:\n")
	sb.WriteString(fmt.Sprintf("  %s updateMode=%s\n", a.Advisory.VPAConflict.VPAName, a.Advisory.VPAConflict.UpdateMode))
	for _, rec := range a.Advisory.VPAConflict.Recommendations {
		sb.WriteString(fmt.Sprintf("  - %s/%s target=%s\n", rec.Container, rec.Resource, rec.Target))
	}
}
