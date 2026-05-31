package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	}

	statusBar := m.renderStatusBar()

	return content + "\n" + statusBar
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
		cursor := "  "
		rowStyle := lipgloss.NewStyle()
		if i == m.cursor {
			cursor = cursorStyle.Render("▸ ")
			rowStyle = lipgloss.NewStyle()
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
			rowStyle.Render(summary)

		sb.WriteString(cursor + row + "\n")
	}

	return sb.String()
}

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
			sb.WriteString(warnStyle.Render(fmt.Sprintf("⚠ %d of %d pods not ready", a.TargetReplicas.NotReady, a.TargetReplicas.TotalReplicas)))
			sb.WriteString("\n")
		}

		// KEDA info.
		if a.KEDAInfo != nil {
			sb.WriteString("\nKEDA:\n")
			sb.WriteString(fmt.Sprintf("  ScaledObject: %s\n", a.KEDAInfo.ScaledObjectName))
			if len(a.KEDAInfo.Triggers) > 0 {
				triggerNames := make([]string, 0, len(a.KEDAInfo.Triggers))
				for _, t := range a.KEDAInfo.Triggers {
					triggerNames = append(triggerNames, t.Type)
				}
				sb.WriteString(fmt.Sprintf("  Triggers: %s\n", strings.Join(triggerNames, ", ")))
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press esc to go back, r to refresh"))

	return sb.String()
}

func (m Model) renderStatusBar() string {
	var parts []string

	if m.paused {
		parts = append(parts, warnStyle.Render("PAUSED"))
	} else {
		parts = append(parts, okStyle.Render("LIVE"))
	}

	parts = append(parts, fmt.Sprintf("interval: %s", m.interval))

	if !m.lastRefresh.IsZero() {
		parts = append(parts, fmt.Sprintf("updated: %s", m.lastRefresh.Format("15:04:05")))
	}

	parts = append(parts, fmt.Sprintf("hpas: %d", len(m.items)))

	if m.filter != "" {
		parts = append(parts, fmt.Sprintf("filter: %s", m.filter))
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
