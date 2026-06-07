package tui

import (
	"fmt"
	"strings"
)

// renderBatchAuditView renders the batch auditor results for selected HPAs.
func (m Model) renderBatchAuditView() string {
	if m.batchAuditState == nil {
		return "No batch audit in progress."
	}

	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" Batch Auditor — Selected HPAs"))
	sb.WriteString("\n\n")

	if m.batchAuditState.loading {
		sb.WriteString(dimStyle.Render("Running auditor on selected HPAs..."))
		sb.WriteString("\n")
		return sb.String()
	}

	if m.batchAuditState.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.batchAuditState.err)))
		sb.WriteString("\n")
	}

	entries := m.batchAuditState.results
	if len(entries) == 0 {
		sb.WriteString(dimStyle.Render("No audit results."))
		sb.WriteString("\n")
		return sb.String()
	}

	// Summary line
	totalCritical := 0
	totalWarnings := 0
	for _, e := range entries {
		totalCritical += e.Critical
		totalWarnings += e.Warnings
	}
	sb.WriteString(fmt.Sprintf("Audited: %d HPAs  |  Critical: %s  Warnings: %s\n\n",
		len(entries),
		severityCount(totalCritical, "critical"),
		severityCount(totalWarnings, "warning"),
	))

	// Table header
	sb.WriteString(dimStyle.Render(
		padRight("NAMESPACE", 12) + "  " +
			padRight("NAME", 24) + "  " +
			padRight("SCORE", 6) + "  " +
			padRight("FINDINGS", 8) + "  " +
			"SUMMARY",
	))
	sb.WriteString("\n")

	// Table rows
	visibleHeight := m.height - 10
	start := 0
	if m.batchAuditState.scrollPos >= visibleHeight {
		start = m.batchAuditState.scrollPos - visibleHeight + 1
	}

	for i := start; i < len(entries) && i < start+visibleHeight; i++ {
		e := entries[i]
		ns := truncate(e.Namespace, 12)
		name := truncate(e.Name, 24)
		score := padRight(fmt.Sprintf("%d", e.Score), 6)
		findings := padRight(fmt.Sprintf("%d", e.Findings), 8)
		summary := truncate(e.Summary, 40)

		scoreSty := okStyle
		if e.Score < 70 {
			scoreSty = warnStyle
		}
		if e.Score < 50 {
			scoreSty = errorStyle
		}

		row := padRight(ns, 12) + "  " +
			padRight(name, 24) + "  " +
			scoreSty.Render(score) + "  " +
			dimStyle.Render(findings) + "  " +
			summary

		sb.WriteString(row)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Press esc to go back"))

	return sb.String()
}

// severityCount returns a styled count string.
func severityCount(count int, severity string) string {
	if count == 0 {
		return dimStyle.Render("0")
	}
	switch severity {
	case "critical":
		return errorStyle.Render(fmt.Sprintf("%d", count))
	case "warning":
		return warnStyle.Render(fmt.Sprintf("%d", count))
	default:
		return fmt.Sprintf("%d", count)
	}
}
