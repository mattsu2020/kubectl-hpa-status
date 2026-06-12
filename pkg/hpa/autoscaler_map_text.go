package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

// WriteAutoscalerMapText writes a standalone autoscaler map visualization.
func WriteAutoscalerMapText(w io.Writer, am *AutoscalerMap, theme style.Theme) error {
	if am == nil {
		return nil
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("Autoscaler map for %s/%s\n\n", am.Namespace, am.HPAName))

	// Render layers as a tree.
	for i, layer := range am.Layers {
		prefix := "  -> "
		if i == 0 {
			prefix = ""
		}

		indicator := theme.OK.Render("✓")
		if !layer.Healthy {
			indicator = theme.Error.Render("✗")
		}

		buf.WriteString(fmt.Sprintf("%s%s %s: %s\n", prefix, indicator, layer.Name, layer.Status))

		for _, detail := range layer.Details {
			buf.WriteString(fmt.Sprintf("     %s\n", theme.Dim.Render(detail)))
		}
	}

	// Risk level.
	if am.Risk != "" && am.Risk != "none" {
		riskBadge := autoscalerRiskBadge(am.Risk, theme)
		buf.WriteString(fmt.Sprintf("\nRisk: %s\n", riskBadge))
	}

	// Blockers.
	if len(am.Blockers) > 0 {
		buf.WriteString("\nBlockers:\n")
		for _, b := range am.Blockers {
			badge := autoscalerBlockerBadge(b.Severity, theme)
			buf.WriteString(fmt.Sprintf("  %s [%s] %s\n", badge, b.Layer, b.Message))
			if b.Detail != "" {
				for _, line := range wrapAutoscalerMapLines(b.Detail, 72) {
					buf.WriteString(fmt.Sprintf("    %s\n", line))
				}
			}
		}
	}

	// Recommendation.
	if am.Recommendation != "" {
		buf.WriteString("\nRecommendation:\n")
		for _, line := range wrapAutoscalerMapLines(am.Recommendation, 76) {
			buf.WriteString(fmt.Sprintf("  %s\n", line))
		}
	}

	// Next actions.
	if len(am.NextActions) > 0 {
		buf.WriteString("\nNext actions:\n")
		for _, action := range am.NextActions {
			buf.WriteString(fmt.Sprintf("  - %s\n", action))
		}
	}

	// Next checks.
	if len(am.NextChecks) > 0 {
		buf.WriteString("\nNext checks:\n")
		for _, check := range am.NextChecks {
			buf.WriteString(fmt.Sprintf("  - %s\n", check))
		}
	}

	_, err := w.Write([]byte(buf.String()))
	return err
}

// autoscalerRiskBadge returns a styled risk level badge.
func autoscalerRiskBadge(risk string, theme style.Theme) string {
	switch risk {
	case "high":
		return theme.Error.Render("high")
	case "medium":
		return theme.Warning.Render("medium")
	case "low":
		return theme.Dim.Render("low")
	default:
		return risk
	}
}

// autoscalerBlockerBadge returns a styled severity badge.
func autoscalerBlockerBadge(severity string, theme style.Theme) string {
	switch severity {
	case "high":
		return theme.Error.Render("[HIGH]")
	case "medium":
		return theme.Warning.Render("[MED]")
	case "low":
		return theme.Dim.Render("[LOW]")
	default:
		return "[INFO]"
	}
}

// wrapAutoscalerMapLines wraps text at word boundaries.
func wrapAutoscalerMapLines(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var current string
	for _, word := range words {
		if current == "" {
			current = word
		} else if len(current)+1+len(word) <= width {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
