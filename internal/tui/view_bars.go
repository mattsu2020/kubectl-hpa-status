package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

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
