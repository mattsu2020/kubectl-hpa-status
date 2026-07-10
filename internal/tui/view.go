package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// View renders the current state of the TUI. Returns a tea.View so the bubble
// tea v2 runtime can manage alt-screen and downsampling; the content itself
// is assembled by viewContent. AltScreen is pinned on the view because v2
// removed the program-level WithAltScreen option.
func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	return v
}

// viewContent assembles the screen content as a plain string. Split out from
// View so the multiple early-return paths stay simple string returns.
func (m Model) viewContent() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.loading && len(m.items) == 0 {
		return "Loading HPA data..."
	}

	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err)) + "\n\nPress q to quit."
	}

	content := m.renderViewByMode()
	statusBar := m.renderStatusBar()

	return content + "\n" + statusBar
}

// renderViewByMode dispatches to the renderer for the current view mode.
func (m Model) renderViewByMode() string {
	switch m.viewMode {
	case listView:
		return m.renderListView()
	case detailView:
		return m.renderDetailView()
	case helpView:
		return m.renderHelpView()
	case metricsView:
		return m.renderMetricsView()
	case simView:
		return m.renderSimView()
	case fixView:
		return m.renderFixView()
	case replayView:
		return m.renderReplayView()
	case batchAuditView:
		return m.renderBatchAuditView()
	case historyView:
		return m.renderHistoryView()
	case overviewView:
		return m.renderOverviewView()
	case hintsView:
		return m.renderHintsView()
	}
	return ""
}

func (m Model) renderHelpView() string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" TUI Help "))
	sb.WriteString("\n\n")

	writeHelpSection(&sb, "Daily triage", []helpBinding{
		{"↑/k", "Move cursor up"},
		{"↓/j", "Move cursor down"},
		{"Enter", "Open HPA detail"},
		{"/", "Filter by name, namespace, health, issue, or summary"},
		{"S", "Cycle sort: name, health-score, issue, namespace"},
		{"g", "Jump to first HPA whose health is not OK"},
		{"O", "Open cluster overview from the list"},
	})

	writeHelpSection(&sb, "Refresh and display", []helpBinding{
		{"r", "Refresh data now"},
		{"p", "Pause or resume auto-refresh"},
		{"+/=", "Decrease refresh interval, minimum 1s"},
		{"-", "Increase refresh interval, maximum 60s"},
		{"Esc", "Go back to the previous dashboard level"},
		{"?", "Toggle this help"},
		{"q/Ctrl+c", "Quit"},
	})

	writeHelpSection(&sb, "Detail drill-down", []helpBinding{
		{"m", "Open per-metric diagnostics"},
		{"s", "Open the what-if simulation panel"},
		{"M", "Toggle parameter and metric-value simulation inside simulation"},
		{"Tab", "Move to the next simulation field"},
		{"Shift+Tab", "Move to the previous simulation field"},
		{"f", "Open the fix wizard when suggestions are available"},
		{"d", "Validate the selected fix with Kubernetes server-side dry-run"},
		{"T", "Open replay timeline from hpa-trace.json"},
		{"H", "Open history and sparkline view"},
		{"h", "Open metric hints troubleshooting when hints are available"},
	})

	writeHelpSection(&sb, "Selection and batch work", []helpBinding{
		{"space", "Toggle current HPA selection"},
		{"a", "Select all visible HPAs"},
		{"A", "Clear the selection"},
		{"B", "Run the batch auditor on selected HPAs"},
		{"x", "Preview and explicitly confirm batch apply for selected HPAs"},
	})

	sb.WriteString(headerStyle.Render(" Export workflow "))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  The TUI keeps export operations explicit. Leave the dashboard and run:"))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  kubectl hpa status <name> -n <namespace> --suggest --export yaml"))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  Other formats: --export kustomize or --export helm-values."))
	sb.WriteString("\n")
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press ? or Esc to close"))
	return sb.String()
}

type helpBinding struct {
	key  string
	desc string
}

func writeHelpSection(sb *strings.Builder, title string, bindings []helpBinding) {
	sb.WriteString(headerStyle.Render(" " + title + " "))
	sb.WriteString("\n")
	for _, b := range bindings {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", b.key, dimStyle.Render(b.desc)))
	}
	sb.WriteString("\n")
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
	nsW, nameW := 12, 20
	healthW, scoreW, stabW, sparkW, issueW := 10, 8, 8, 15, 14
	summaryW := availWidth - nsW - nameW - healthW - scoreW - stabW - sparkW - issueW - 7*2 // 7 gaps
	if summaryW < 10 {
		summaryW = 10
	}

	var sb strings.Builder
	if m.batchApplyConfirm {
		sb.WriteString(headerStyle.Render("Batch apply preview"))
		sb.WriteString("\n")
		for _, line := range m.batchApplyPreview {
			sb.WriteString(fmt.Sprintf("  - %s\n", line))
		}
		if m.opts.ApplyFn == nil {
			sb.WriteString(dimStyle.Render("Live apply is disabled; restart with --apply --dry-run=false to enable it. Esc=cancel."))
		} else {
			sb.WriteString(dimStyle.Render("Press x again to apply, Esc to cancel."))
		}
		sb.WriteString("\n\n")
	}

	// Header.
	sb.WriteString(dimStyle.Render(
		padRight("NAMESPACE", nsW) + "  " +
			padRight("NAME", nameW) + "  " +
			padRight("HEALTH", healthW) + "  " +
			padRight("SCORE", scoreW) + "  " +
			padRight("STAB", stabW) + "  " +
			padRight("TREND", sparkW) + "  " +
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
		score := renderMiniScoreBar(item.HealthScore, scoreW)
		stab := renderStabBadge(item, stabW)

		// Inline sparkline from replica history.
		sparkline := renderInlineSparkline(m.replicaHistory[itemKey], sparkW, item.ChurnLevel)

		// Churn indicator prefix for ISSUE column.
		issueText := truncate(item.Issue, issueW)
		switch item.ChurnLevel {
		case "HIGH", "CRITICAL":
			issueText = errorStyle.Render("●") + " " + truncate(item.Issue, issueW-4)
		case "MEDIUM":
			issueText = warnStyle.Render("⚠") + " " + truncate(item.Issue, issueW-4)
		}

		summary := truncate(item.Summary, summaryW)

		row := padRight(ns, nsW) + "  " +
			padRight(name, nameW) + "  " +
			health + "  " +
			score + "  " +
			padRight(stab, stabW) + "  " +
			sparkline + "  " +
			padRight(issueText, issueW) + "  " +
			summary

		sb.WriteString(cursor + row + "\n")
	}

	return sb.String()
}

// renderDetailView has moved to view_detail.go, split into per-section helpers
// to keep nesting shallow. See renderDetail* in that file.

// renderMetricsView renders the per-metric detail breakdown for the selected HPA.

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
		appendMetricsReportBody(&sb, report.Analysis)
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

	switch m.viewMode {
	case listView:
		parts = append(parts, "↑↓ navigate  enter detail  / filter  ? help  p pause  q quit")
	case detailView:
		parts = append(parts, "m metrics  s simulate  f fix  H history  h hints  ? help  esc back")
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

// renderEnhancedCountdownBar renders a color-graded stabilization countdown bar
// with human-readable duration labels. The bar color transitions from green
// (>60% remaining) to yellow (30–60%) to red (<30%).
func renderEnhancedCountdownBar(remaining int, total int) string {
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

	remainingRatio := float64(remaining) / float64(total)
	var style lipgloss.Style
	switch {
	case remainingRatio > 0.6:
		style = okStyle
	case remainingRatio > 0.3:
		style = warnStyle
	default:
		style = errorStyle
	}

	label := hpaanalysis.FormatStabilizationProgress(
		ptrInt64(int64(remaining)), ptrInt32(int32(total)),
	)
	return style.Render(bar) + " " + dimStyle.Render(label)
}

func ptrInt64(v int64) *int64 { return &v }
func ptrInt32(v int32) *int32 { return &v }

// renderMiniScoreBar renders a compact colored bar for the list view SCORE column.
// Width is typically 8 characters.
func renderMiniScoreBar(score int, width int) string {
	if width <= 0 {
		width = 8
	}
	barW := width - 3 // reserve 3 chars for the numeric score
	if barW < 3 {
		barW = 3
	}
	filled := score * barW / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barW {
		filled = barW
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
	var style lipgloss.Style
	switch {
	case score >= 80:
		style = okStyle
	case score >= 50:
		style = warnStyle
	default:
		style = errorStyle
	}
	return style.Render(bar) + fmt.Sprintf("%3d", score)
}

// renderStabBadge renders the stabilization countdown badge for a list row.
func renderStabBadge(item hpaanalysis.ListItem, width int) string {
	if !item.Stabilizing || item.StabilizationLabel == "" {
		return dimStyle.Render(padRight("-", width))
	}
	label := item.StabilizationLabel
	if len(label) > width {
		label = label[:width]
	}
	return warnStyle.Render(padRight(label, width))
}

// renderInlineSparkline renders a compact sparkline for the list view TREND column.
func renderInlineSparkline(history []float64, width int, churnLevel string) string {
	if len(history) < 2 {
		return dimStyle.Render(padRight("·", width))
	}
	var style lipgloss.Style
	switch churnLevel {
	case "HIGH", "CRITICAL":
		style = errorStyle
	case "MEDIUM":
		style = warnStyle
	default:
		style = okStyle
	}
	return renderSparkline(history, width, style)
}

// appendMetricsReportBody renders the per-metric rows and pipeline diagnostics section of the metrics view.
func appendMetricsReportBody(sb *strings.Builder, a hpaanalysis.Analysis) {
	if len(a.Metrics) == 0 {
		sb.WriteString(dimStyle.Render("No metrics configured for this HPA."))
		sb.WriteString("\n")
	}

	for i, metric := range a.Metrics {
		appendMetricRow(sb, metric, i, len(a.Metrics))
	}

	if a.MetricsDiagnostics != nil {
		appendMetricsPipelineDiagnostics(sb, a.MetricsDiagnostics)
	}
}

func appendMetricRow(sb *strings.Builder, metric hpaanalysis.Metric, i, total int) {
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

	if i < total-1 {
		sb.WriteString("\n")
	}
}

func appendMetricsPipelineDiagnostics(sb *strings.Builder, diags *hpaanalysis.MetricsPipelineDiagnostics) {
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render(" Pipeline Health "))
	sb.WriteString("\n\n")

	for _, check := range diags.PerMetricChecks {
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

	if len(diags.RemediationSteps) > 0 {
		sb.WriteString("\n")
		sb.WriteString("Remediation:\n")
		for _, step := range diags.RemediationSteps {
			sb.WriteString(fmt.Sprintf("  - %s\n", step))
		}
	}
}
