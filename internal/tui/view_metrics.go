package tui

import (
	"fmt"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

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
