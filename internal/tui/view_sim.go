package tui

import (
	"fmt"
	"strings"
)

// renderSimView renders the interactive simulation panel.
func (m Model) renderSimView() string {
	if m.simState == nil {
		return dimStyle.Render("No simulation data. Press s from detail view.")
	}

	var sb strings.Builder

	// Header.
	sb.WriteString(headerStyle.Render(fmt.Sprintf("HPA Simulation: %s/%s", m.simState.hpa.Namespace, m.simState.hpa.Name)))
	sb.WriteString("\n")

	// Mode indicator.
	modeLabel := "Parameters"
	if m.simState.metricMode {
		modeLabel = "Metric Values"
	}
	sb.WriteString(dimStyle.Render(fmt.Sprintf("Mode: %s  [M to toggle]", modeLabel)))
	sb.WriteString("\n\n")

	if m.simState.metricMode {
		sb.WriteString(m.renderMetricSimInput())
	} else {
		sb.WriteString(m.renderParamSimFields())
	}

	// Simulation result.
	if m.simState.err != nil {
		sb.WriteString("\n")
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.simState.err)))
		sb.WriteString("\n")
	}
	if m.simState.result != nil {
		sb.WriteString(m.renderSimResult())
	}

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Enter=simulate  a=apply  M=toggle metric  Esc=back"))

	return sb.String()
}

// renderParamSimFields renders the parameter input fields for HPA simulation.
func (m Model) renderParamSimFields() string {
	var sb strings.Builder
	sb.WriteString("Parameters [Tab to cycle]:\n")

	for i, field := range m.simState.fields {
		marker := "  "
		if i == m.simState.focusIndex {
			marker = cursorStyle.Render("> ")
		}

		valueStr := field.Value
		if valueStr == "" {
			valueStr = field.Original
		}

		sb.WriteString(fmt.Sprintf("%s %-42s [%s] (current: %s)\n",
			marker,
			field.Label,
			padRight(valueStr, 10),
			field.Original,
		))
	}

	return sb.String()
}

// renderMetricSimInput renders the metric value simulation input.
func (m Model) renderMetricSimInput() string {
	var sb strings.Builder
	sb.WriteString("Metric Override [e.g. cpu=80%, memory=4Gi, http_requests=+20%]:\n")
	sb.WriteString("  " + m.simState.metricInput.View())
	sb.WriteString("\n")
	return sb.String()
}

// renderSimResult renders the before/after simulation comparison.
func (m Model) renderSimResult() string {
	r := m.simState.result
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("--- Simulation Result ---"))
	sb.WriteString("\n")

	if r.Parameter != "" {
		sb.WriteString(fmt.Sprintf("  Parameter: %s\n", r.Parameter))
		if r.OriginalValue != "" || r.SimulatedValue != "" {
			sb.WriteString(fmt.Sprintf("  %s → %s\n", r.OriginalValue, r.SimulatedValue))
		}
	}

	// Before/after comparison.
	sb.WriteString(fmt.Sprintf("  Before: desired=%d  health=%s (%d)\n",
		r.Before.DesiredReplicas, r.Before.Health, r.Before.HealthScore))
	sb.WriteString(fmt.Sprintf("  After:  desired=%d  health=%s (%d)\n",
		r.After.DesiredReplicas, r.After.Health, r.After.HealthScore))

	// Health change highlight.
	if r.Before.Health != r.After.Health {
		sb.WriteString(fmt.Sprintf("  %s health: %s → %s\n",
			warnStyle.Render("⚠"),
			r.Before.Health,
			healthStyle(r.After.Health).Render(r.After.Health),
		))
	}

	// Metric simulations (from --simulate-metric).
	for _, ms := range r.MetricSimulations {
		sb.WriteString(fmt.Sprintf("  Metric %s: %s → %s", ms.MetricName, ms.OriginalValue, ms.SimulatedValue))
		if ms.ProjectedReplicas > 0 {
			sb.WriteString(fmt.Sprintf("  (projected replicas: %d)", ms.ProjectedReplicas))
		}
		sb.WriteString("\n")
	}

	// Risk assessment.
	if r.RiskAssessment != "" {
		sb.WriteString(fmt.Sprintf("  Risk: %s\n", warnStyle.Render(r.RiskAssessment)))
	}

	// Interpretation.
	for _, line := range r.Interpretation {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  %s\n", line)))
	}

	return sb.String()
}
