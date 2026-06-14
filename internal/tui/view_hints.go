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
	selected := clampHintsSelection(m.hintsState.selected, len(m.hintsState.flows))
	appendHintsList(&sb, m.hintsState.flows, selected)

	// 3. Selected hint's step-by-step commands.
	if selected >= 0 && selected < len(m.hintsState.flows) {
		appendHintsSteps(&sb, m.hintsState.flows[selected], m.hintsState.stepScroll)
	}

	// 4. Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("↑/↓ select | esc back"))

	return sb.String()
}

func clampHintsSelection(selected, flowCount int) int {
	if selected < 0 {
		return 0
	}
	if selected >= flowCount {
		return flowCount - 1
	}
	return selected
}

func appendHintsList(sb *strings.Builder, flows []hpaanalysis.MetricHintTroubleshooting, selected int) {
	for i, flow := range flows {
		prefix := "  "
		if i == selected {
			prefix = cursorStyle.Render("▸ ")
		}

		badge := severityBadge(flow.Severity)

		label := fmt.Sprintf("%s %s", flow.Title, dimStyle.Render(fmt.Sprintf("(%s/%s)", flow.MetricType, flow.MetricName)))

		sb.WriteString(fmt.Sprintf("%s%s %s\n", prefix, badge, label))
	}
}

func appendHintsSteps(sb *strings.Builder, flow hpaanalysis.MetricHintTroubleshooting, stepScroll int) {
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("  Steps:"))
	sb.WriteString("\n")

	maxStep := len(flow.Steps)
	startStep, endStep, visibleSteps := hintsStepWindow(stepScroll, maxStep)

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

// hintsStepWindow computes the visible step range for the troubleshooting flow, scrolling from the end.
func hintsStepWindow(stepScroll, maxStep int) (startStep, endStep, visibleSteps int) {
	startStep = stepScroll
	if startStep < 0 {
		startStep = 0
	}
	visibleSteps = maxStep
	if visibleSteps > 8 {
		visibleSteps = 8
	}
	if startStep+visibleSteps > maxStep {
		startStep = maxStep - visibleSteps
		if startStep < 0 {
			startStep = 0
		}
	}
	endStep = startStep + visibleSteps
	if endStep > maxStep {
		endStep = maxStep
	}
	return startStep, endStep, visibleSteps
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
