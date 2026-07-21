package tui

import (
	"fmt"
	"strings"
)

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
