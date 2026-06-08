package tui

import (
	"fmt"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// renderHintsView renders the metric hints troubleshooting flow for the
// selected HPA. It shows a list of detected hint patterns with severity
// badges, and the selected hint's step-by-step diagnostic commands.
func (m Model) renderHintsView() string {
	items := m.filteredItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return headerStyle.Render("Metric Troubleshooting") + "\n\n" +
			dimStyle.Render("No HPA selected.")
	}

	item := items[m.cursor]

	if m.hintsState == nil || len(m.hintsState.flows) == 0 {
		return headerStyle.Render(fmt.Sprintf("Metric Troubleshooting: %s/%s", item.Namespace, item.Name)) +
			"\n\n" + dimStyle.Render("No metric hints available. Use --metric-hints flag to enable detection.")
	}

	var sb strings.Builder

	// 1. Header.
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Metric Troubleshooting: %s/%s", item.Namespace, item.Name)))
	sb.WriteString("\n\n")

	// 2. List of hints with severity badges.
	selected := m.hintsState.selected
	if selected < 0 {
		selected = 0
	}
	if selected >= len(m.hintsState.flows) {
		selected = len(m.hintsState.flows) - 1
	}

	for i, flow := range m.hintsState.flows {
		prefix := "  "
		if i == selected {
			prefix = cursorStyle.Render("▸ ")
		}

		badge := severityBadge(flow.Severity)

		label := fmt.Sprintf("%s %s", flow.Title, dimStyle.Render(fmt.Sprintf("(%s/%s)", flow.MetricType, flow.MetricName)))

		sb.WriteString(fmt.Sprintf("%s%s %s\n", prefix, badge, label))
	}

	// 3. Selected hint's step-by-step commands.
	if selected >= 0 && selected < len(m.hintsState.flows) {
		flow := m.hintsState.flows[selected]
		sb.WriteString("\n")
		sb.WriteString(headerStyle.Render("  Steps:"))
		sb.WriteString("\n")

		maxStep := len(flow.Steps)
		startStep := m.hintsState.stepScroll
		if startStep < 0 {
			startStep = 0
		}
		visibleSteps := maxStep
		if visibleSteps > 8 {
			visibleSteps = 8
		}
		if startStep+visibleSteps > maxStep {
			startStep = maxStep - visibleSteps
			if startStep < 0 {
				startStep = 0
			}
		}
		endStep := startStep + visibleSteps
		if endStep > maxStep {
			endStep = maxStep
		}

		for _, step := range flow.Steps[startStep:endStep] {
			sb.WriteString(fmt.Sprintf("    %d. %s\n", step.StepNumber, step.Description))
			if step.Command != "" {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("       $ %s\n", step.Command)))
			}
			if step.ExpectedOutput != "" {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("          expect: %s\n", step.ExpectedOutput)))
			}
			if step.DocsLink != "" {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("          docs: %s\n", step.DocsLink)))
			}
		}

		if maxStep > visibleSteps {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("    [%d-%d of %d steps]\n", startStep+1, endStep, maxStep)))
		}
	}

	// 4. Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("↑/↓ select | esc back"))

	return sb.String()
}

// severityBadge returns a styled severity indicator for the hint list.
func severityBadge(severity string) string {
	switch severity {
	case "error":
		return errorStyle.Render("●")
	case "warning":
		return warnStyle.Render("●")
	default:
		return dimStyle.Render("●")
	}
}

// renderHintsText renders a plain-text troubleshooting section for the
// StatusReport text output. This is used by the non-TUI text renderer.
func renderHintsText(flows []hpaanalysis.MetricHintTroubleshooting) string {
	if len(flows) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Metric Troubleshooting:\n")
	for _, flow := range flows {
		sb.WriteString(fmt.Sprintf("  [%s] %s (%s/%s)\n", flow.Severity, flow.Title, flow.MetricType, flow.MetricName))
		for _, step := range flow.Steps {
			sb.WriteString(fmt.Sprintf("    %d. %s\n", step.StepNumber, step.Description))
			if step.Command != "" {
				sb.WriteString(fmt.Sprintf("       $ %s\n", step.Command))
			}
		}
	}
	return sb.String()
}
