package tui

import (
	"fmt"
	"strings"
)

// View renders the current state of the TUI.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.loading && len(m.items) == 0 {
		return "Loading HPA data..."
	}

	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err)) + "\n\nPress q to quit."
	}

	var content string
	switch m.viewMode {
	case listView:
		content = m.renderListView()
	case detailView:
		content = m.renderDetailView()
	case helpView:
		content = m.renderHelpView()
	case metricsView:
		content = m.renderMetricsView()
	case simView:
		content = m.renderSimView()
	case fixView:
		content = m.renderFixView()
	case replayView:
		content = m.renderReplayView()
	case batchAuditView:
		content = m.renderBatchAuditView()
	}

	statusBar := m.renderStatusBar()

	return content + "\n" + statusBar
}

func (m Model) renderHelpView() string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" Key Bindings "))
	sb.WriteString("\n\n")
	bindings := []struct{ key, desc string }{
		{"↑/k", "Move cursor up"},
		{"↓/j", "Move cursor down"},
		{"Enter", "View HPA detail"},
		{"Esc", "Go back / Close help"},
		{"/", "Filter by name, namespace, health, or issue"},
		{"S", "Cycle sort: name → health-score → issue → namespace"},
		{"g", "Jump to first problematic HPA"},
		{"space", "Toggle select current HPA"},
		{"a", "Select all visible HPAs"},
		{"A", "Deselect all"},
		{"B", "Batch auditor on selected HPAs"},
		{"x", "Batch apply patches to selected HPAs"},
		{"s", "Open simulation panel"},
		{"f", "Open fix wizard"},
		{"T", "Open replay timeline"},
		{"M", "Toggle metric simulation mode"},
		{"Tab", "Cycle simulation fields"},
		{"r", "Refresh data now"},
		{"p", "Pause/resume auto-refresh"},
		{"+/=", "Decrease refresh interval (faster)"},
		{"-", "Increase refresh interval (slower)"},
		{"?", "Toggle this help"},
		{"q/Ctrl+c", "Quit"},
	}
	for _, b := range bindings {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", b.key, dimStyle.Render(b.desc)))
	}
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press ? or Esc to close"))
	return sb.String()
}

// tuiTriggerStatusBadge returns a styled status string for a KEDA trigger.
func tuiTriggerStatusBadge(status string) string {
	switch status {
	case "Active":
		return okStyle.Render("Active ✓")
	case "Inactive":
		return errorStyle.Render("Inactive ✗")
	default:
		return dimStyle.Render("Unknown ?")
	}
}

func (m Model) renderListView() string {
	filtered := m.filteredItems()
	if len(filtered) == 0 {
		msg := "No HPAs found."
		if m.filter != "" {
			msg = fmt.Sprintf("No HPAs matching filter %q.", m.filter)
		}
		return msg
	}

	// Calculate column widths.
	availWidth := m.width - 2 // account for cursor marker
	nsW, nameW := 12, 24
	healthW, scoreW, issueW := 10, 6, 20
	summaryW := availWidth - nsW - nameW - healthW - scoreW - issueW - 5*2 // 5 gaps
	if summaryW < 10 {
		summaryW = 10
	}

	var sb strings.Builder

	// Header.
	sb.WriteString(dimStyle.Render(
		padRight("NAMESPACE", nsW) + "  " +
			padRight("NAME", nameW) + "  " +
			padRight("HEALTH", healthW) + "  " +
			padRight("SCORE", scoreW) + "  " +
			padRight("ISSUE", issueW) + "  " +
			"SUMMARY",
	))
	sb.WriteString("\n")

	// Rows.
	visibleHeight := m.height - 4 // header + status bar + padding
	start := 0
	if m.cursor >= visibleHeight {
		start = m.cursor - visibleHeight + 1
	}

	for i := start; i < len(filtered) && i < start+visibleHeight; i++ {
		item := filtered[i]
		// Checkbox for selection.
		itemKey := item.Namespace + "/" + item.Name
		var cursor string
		if m.selected[itemKey] {
			cursor = cursorStyle.Render("▸ ") + okStyle.Render("[x] ")
		} else {
			cursor = cursorStyle.Render("▸ ") + dimStyle.Render("[ ] ")
		}

		ns := truncate(item.Namespace, nsW)
		name := truncate(item.Name, nameW)
		health := healthStyle(item.Health).Render(padRight(item.Health, healthW))
		score := padRight(fmt.Sprintf("%d", item.HealthScore), scoreW)
		issue := truncate(item.Issue, issueW)
		summary := truncate(item.Summary, summaryW)

		row := padRight(ns, nsW) + "  " +
			padRight(name, nameW) + "  " +
			health + "  " +
			dimStyle.Render(score) + "  " +
			dimStyle.Render(padRight(issue, issueW)) + "  " +
			summary

		sb.WriteString(cursor + row + "\n")
	}

	return sb.String()
}

//nolint:gocyclo // Sequential rendering of multiple optional detail sections; each section is independent.
func (m Model) renderDetailView() string {
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return "No HPA selected."
	}

	item := filtered[m.cursor]
	key := item.Namespace + "/" + item.Name

	var sb strings.Builder

	// Header.
	sb.WriteString(headerStyle.Render(fmt.Sprintf("HPA %s/%s", item.Namespace, item.Name)))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("Target: %s", item.Target)))
	sb.WriteString("\n\n")

	// Health bar.
	healthLabel := healthStyle(item.Health).Render(item.Health)
	scoreBar := renderScoreBar(item.HealthScore)
	sb.WriteString(fmt.Sprintf("Health: %s  %s  %d/100", healthLabel, scoreBar, item.HealthScore))
	sb.WriteString("\n\n")

	// Replicas.
	diff := item.Desired - item.Current
	diffStr := fmt.Sprintf("%+d", diff)
	sb.WriteString(fmt.Sprintf("Replicas: current=%d  desired=%d  diff=%s  min=%d  max=%d",
		item.Current, item.Desired, diffStr, item.Min, item.Max))
	sb.WriteString("\n\n")

	// Summary.
	sb.WriteString("Summary: ")
	sb.WriteString(item.Summary)
	sb.WriteString("\n")

	// Report detail if available.
	report, ok := m.reports[key]
	if ok && report != nil {
		a := report.Analysis

		// Stabilization countdown.
		if a.StabilizationRemaining != nil && *a.StabilizationRemaining > 0 {
			remaining := fmt.Sprintf("%ds", *a.StabilizationRemaining)
			sb.WriteString("\n")
			sb.WriteString(warnStyle.Render(fmt.Sprintf("Scale-down stabilized: %s remaining", remaining)))
			sb.WriteString("\n")
			if a.StabilizationWindowSeconds != nil && *a.StabilizationWindowSeconds > 0 {
				bar := renderCountdownBar(int(*a.StabilizationRemaining), int(*a.StabilizationWindowSeconds))
				sb.WriteString(bar)
				sb.WriteString("\n")
			}
		}

		// Conditions.
		if len(a.Conditions) > 0 {
			sb.WriteString("\nConditions:\n")
			for _, c := range a.Conditions {
				statusStyle := okStyle
				if c.Status != "True" {
					statusStyle = errorStyle
				}
				sb.WriteString(fmt.Sprintf("  %-15s %s %s\n",
					c.Type,
					statusStyle.Render(c.Status),
					dimStyle.Render(c.Reason),
				))
			}
		}

		// Metrics.
		if len(a.Metrics) > 0 {
			sb.WriteString("\nMetrics:\n")
			for _, metric := range a.Metrics {
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

		// Actions.
		if len(a.Actions) > 0 {
			sb.WriteString("\nActions:\n")
			for _, action := range a.Actions {
				sb.WriteString(fmt.Sprintf("  • %s\n", action))
			}
		}

		// Interpretation (compact).
		if len(a.Interpretation) > 0 {
			sb.WriteString("\nInterpretation:\n")
			maxLines := 5
			for i, line := range a.Interpretation {
				if i >= maxLines {
					sb.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more\n", len(a.Interpretation)-maxLines)))
					break
				}
				sb.WriteString(dimStyle.Render("  " + line + "\n"))
			}
		}

		// Target replica info (not-ready pods).
		if a.TargetReplicas != nil && a.TargetReplicas.NotReady > 0 {
			sb.WriteString("\n")
			sb.WriteString(warnStyle.Render(fmt.Sprintf("%d of %d pods not ready", a.TargetReplicas.NotReady, a.TargetReplicas.TotalReplicas)))
			sb.WriteString("\n")
		}
		if a.TargetReplicas != nil && a.TargetReplicas.Pending > 0 {
			sb.WriteString("\n")
			sb.WriteString(warnStyle.Render(fmt.Sprintf("%d pods pending (%d unschedulable)", a.TargetReplicas.Pending, a.TargetReplicas.Unschedulable)))
			sb.WriteString("\n")
		}

		// KEDA info.
		if a.KEDAInfo != nil {
			sb.WriteString("\nKEDA:\n")
			sb.WriteString(fmt.Sprintf("  ScaledObject: %s\n", a.KEDAInfo.ScaledObjectName))
			if len(a.KEDAInfo.Triggers) > 0 {
				sb.WriteString("  Triggers:\n")
				for _, t := range a.KEDAInfo.Triggers {
					label := t.Type
					if t.Name != "" {
						label = fmt.Sprintf("%s (%s)", t.Type, t.Name)
					}
					if t.Status != "" {
						badge := tuiTriggerStatusBadge(t.Status)
						label = fmt.Sprintf("%s: %s", label, badge)
					}
					sb.WriteString(fmt.Sprintf("    - %s\n", label))
					if t.MetricName != "" || t.Threshold != "" || t.CurrentValue != "" {
						var detailParts []string
						if t.MetricName != "" {
							detailParts = append(detailParts, fmt.Sprintf("metric=%s", t.MetricName))
						}
						if t.Threshold != "" {
							detailParts = append(detailParts, fmt.Sprintf("threshold=%s", t.Threshold))
						}
						if t.CurrentValue != "" {
							detailParts = append(detailParts, fmt.Sprintf("current=%s", t.CurrentValue))
						}
						sb.WriteString(fmt.Sprintf("      %s\n", strings.Join(detailParts, " ")))
					}
					if t.AuthRef != "" {
						sb.WriteString(fmt.Sprintf("      authRef=%s\n", t.AuthRef))
					}
				}
			}
			if a.KEDAInfo.PollingInterval != nil {
				sb.WriteString(fmt.Sprintf("  Polling interval: %ds\n", *a.KEDAInfo.PollingInterval))
			}
			if a.KEDAInfo.CooldownPeriod != nil {
				sb.WriteString(fmt.Sprintf("  Cooldown period: %ds\n", *a.KEDAInfo.CooldownPeriod))
			}
			if a.KEDAInfo.Fallback != nil {
				sb.WriteString(fmt.Sprintf("  Fallback: failureThreshold=%d, replicas=%d\n", a.KEDAInfo.Fallback.FailureThreshold, a.KEDAInfo.Fallback.Replicas))
			}
		}

		if len(a.Suggestions) > 0 {
			sb.WriteString("\nSuggestions:\n")
			for _, suggestion := range a.Suggestions {
				sb.WriteString(fmt.Sprintf("  - %s (%s)\n", suggestion.Title, suggestion.Risk))
			}
			sb.WriteString(dimStyle.Render("  Use --fix --apply for the selected HPA to validate patches.\n"))
		}

		if a.VPAConflict != nil {
			sb.WriteString("\nVPA:\n")
			sb.WriteString(fmt.Sprintf("  %s updateMode=%s\n", a.VPAConflict.VPAName, a.VPAConflict.UpdateMode))
			for _, rec := range a.VPAConflict.Recommendations {
				sb.WriteString(fmt.Sprintf("  - %s/%s target=%s\n", rec.Container, rec.Resource, rec.Target))
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press esc to go back, r to refresh, m metrics detail"))

	return sb.String()
}

// renderMetricsView shows detailed per-metric diagnostics for the selected HPA.
func (m Model) renderMetricsView() string {
	filtered := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(filtered) {
		return "No HPA selected."
	}

	item := filtered[m.cursor]
	key := item.Namespace + "/" + item.Name

	var sb strings.Builder
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Metrics Diagnostics: %s/%s", item.Namespace, item.Name)))
	sb.WriteString("\n\n")

	report, ok := m.reports[key]
	if !ok || report == nil {
		sb.WriteString(dimStyle.Render("No detailed metrics data available."))
		sb.WriteString("\n")
	} else {
		a := report.Analysis

		if len(a.Metrics) == 0 {
			sb.WriteString(dimStyle.Render("No metrics configured for this HPA."))
			sb.WriteString("\n")
		}

		for i, metric := range a.Metrics {
			name := metric.Name
			if name == "" {
				name = metric.Type
			}

			sb.WriteString(fmt.Sprintf("  %s (%s)\n", name, metric.Type))

			if metric.Current != "" {
				sb.WriteString(fmt.Sprintf("    current=%s", metric.Current))
			}
			if metric.Target != "" {
				sb.WriteString(fmt.Sprintf("  target=%s", metric.Target))
			}
			if metric.Ratio != nil {
				sb.WriteString(fmt.Sprintf("  ratio=%.3f", *metric.Ratio))
			}
			sb.WriteString("\n")

			// Status assessment.
			if metric.Ratio != nil {
				ratio := *metric.Ratio
				switch {
				case ratio > 1.0:
					sb.WriteString(fmt.Sprintf("    status: %s\n", warnStyle.Render("above target")))
				case ratio < 0.9:
					sb.WriteString(fmt.Sprintf("    status: %s\n", okStyle.Render("below target")))
				default:
					sb.WriteString(fmt.Sprintf("    status: %s\n", okStyle.Render("within tolerance")))
				}
			}

			// Note contains diagnostic info.
			if metric.Note != "" {
				sb.WriteString(fmt.Sprintf("    note: %s\n", dimStyle.Render(metric.Note)))
			}

			if i < len(a.Metrics)-1 {
				sb.WriteString("\n")
			}
		}

		// Metrics pipeline diagnostics if available.
		if a.MetricsDiagnostics != nil {
			sb.WriteString("\n")
			sb.WriteString(headerStyle.Render(" Pipeline Health "))
			sb.WriteString("\n\n")

			for _, check := range a.MetricsDiagnostics.PerMetricChecks {
				statusStr := okStyle.Render("✓")
				switch check.Status {
				case "missing":
					statusStr = errorStyle.Render("✗")
				case "stale":
					statusStr = warnStyle.Render("⚠")
				}
				sb.WriteString(fmt.Sprintf("  %s %s/%s: %s\n", statusStr, check.MetricType, check.MetricName, check.Status))
				if check.Details != "" {
					sb.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(check.Details)))
				}
				if check.Remediation != "" {
					sb.WriteString(fmt.Sprintf("    fix: %s\n", dimStyle.Render(check.Remediation)))
				}
			}

			if len(a.MetricsDiagnostics.RemediationSteps) > 0 {
				sb.WriteString("\n")
				sb.WriteString("Remediation:\n")
				for _, step := range a.MetricsDiagnostics.RemediationSteps {
					sb.WriteString(fmt.Sprintf("  - %s\n", step))
				}
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press esc to go back"))
	return sb.String()
}

func (m Model) renderStatusBar() string {
	var parts []string

	if m.paused {
		parts = append(parts, warnStyle.Render("PAUSED"))
	} else {
		parts = append(parts, okStyle.Render("LIVE"))
	}

	parts = append(parts, fmt.Sprintf("interval: %s  (+/-)", m.interval))

	if !m.lastRefresh.IsZero() {
		parts = append(parts, fmt.Sprintf("updated: %s", m.lastRefresh.Format("15:04:05")))
	}

	parts = append(parts, fmt.Sprintf("hpas: %d", len(m.items)))
	if len(m.selected) > 0 {
		parts = append(parts, fmt.Sprintf("selected: %d", len(m.selected)))
	}

	if m.filter != "" {
		parts = append(parts, fmt.Sprintf("filter: %s", m.filter))
	}

	if m.sortField != "" {
		parts = append(parts, fmt.Sprintf("sort:%s", m.sortField))
	}

	if m.viewMode == listView {
		parts = append(parts, "↑↓ navigate  enter detail  / filter  p pause  q quit")
	}

	if m.filtering {
		return fmt.Sprintf("\n%s", m.filterInput.View())
	}

	return statusBarStyle.Render(strings.Join(parts, "  ·  "))
}

func renderScoreBar(score int) string {
	const width = 20
	filled := score * width / 100
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	switch {
	case score >= 80:
		return okStyle.Render(bar)
	case score >= 50:
		return warnStyle.Render(bar)
	default:
		return errorStyle.Render(bar)
	}
}

// renderCountdownBar renders a visual bar showing stabilization window progress.
func renderCountdownBar(remaining, total int) string {
	const width = 20
	if total <= 0 {
		return ""
	}
	elapsed := total - remaining
	if elapsed < 0 {
		elapsed = 0
	}
	filled := elapsed * width / total
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return warnStyle.Render(bar) + dimStyle.Render(fmt.Sprintf(" %ds/%ds", remaining, total))
}
