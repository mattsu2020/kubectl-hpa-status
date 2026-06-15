package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// WriteMetricContractText writes a metrics contract report in plain text.
func WriteMetricContractText(w io.Writer, report *MetricContractReport) error {
	var out []byte
	appendMetricContractText(&out, report, style.Theme{})
	_, err := w.Write(out)
	return err
}

// AppendMetricContractText appends a metrics contract report to a byte slice.
func appendMetricContractText(out *[]byte, report *MetricContractReport, theme style.Theme) {
	*out = fmt.Appendf(*out, "Metrics Contract: %s/%s (%s)\n", report.Namespace, report.Name, report.Target)

	// Overall status
	statusLabel := metricContractStatus(report.OverallStatus, theme)
	*out = fmt.Appendf(*out, "Overall: %s\n", statusLabel)
	*out = fmt.Appendf(*out, "Summary: %s\n", report.Summary)

	if len(report.Remediation) > 0 {
		*out = append(*out, "\nRemediation:\n"...)
		for _, step := range report.Remediation {
			*out = fmt.Appendf(*out, "  - %s\n", theme.ActionLine(step))
		}
	}

	// Individual checks
	if len(report.Checks) > 0 {
		*out = append(*out, "\nChecks:\n"...)
		for i, check := range report.Checks {
			indicator := metricContractIndicator(check.Status, theme)
			*out = fmt.Appendf(*out, "%d. [%s] %s/%s\n", i+1, indicator, check.MetricType, check.MetricName)

			// APIService status
			apiStatus := "Available"
			if !check.APIServiceAvailable {
				apiStatus = "Unavailable"
			}
			*out = fmt.Appendf(*out, "   APIService: %s — %s\n", check.APIService, apiStatus)
			if check.APIServiceMessage != "" {
				*out = fmt.Appendf(*out, "   Message: %s\n", check.APIServiceMessage)
			}

			// Data status
			if !check.DataAvailable {
				*out = fmt.Appendf(*out, "   Data: not available")
				if check.DataMessage != "" {
					*out = fmt.Appendf(*out, " (%s)", check.DataMessage)
				}
				*out = append(*out, '\n')
			}

			// Detail and remediation
			if check.Detail != "" {
				*out = fmt.Appendf(*out, "   Detail: %s\n", check.Detail)
			}
			if check.Remediation != "" {
				*out = fmt.Appendf(*out, "   Remediation: %s\n", theme.ActionLine(check.Remediation))
			}

			if i < len(report.Checks)-1 {
				*out = append(*out, '\n')
			}
		}
	}
}

// WriteMetricContractMarkdown writes a metrics contract report in Markdown format.
func WriteMetricContractMarkdown(w io.Writer, report *MetricContractReport) error {
	var out []byte

	out = fmt.Appendf(out, "## Metrics Contract: %s/%s (%s)\n\n", report.Namespace, report.Name, report.Target)

	// Overall status
	out = fmt.Appendf(out, "**Overall Status:** %s\n\n", report.OverallStatus)
	out = fmt.Appendf(out, "**Summary:** %s\n\n", report.Summary)

	if len(report.Remediation) > 0 {
		out = append(out, "### Remediation\n\n"...)
		for _, step := range report.Remediation {
			out = fmt.Appendf(out, "- %s\n", step)
		}
		out = append(out, '\n')
	}

	// Individual checks
	if len(report.Checks) > 0 {
		out = append(out, "### Checks\n\n"...)
		for i, check := range report.Checks {
			out = fmt.Appendf(out, "%d. **%s/%s** — %s\n", i+1, check.MetricType, check.MetricName, check.Status)

			// APIService status
			apiStatus := "Available"
			if !check.APIServiceAvailable {
				apiStatus = "Unavailable"
			}
			out = fmt.Appendf(out, "   - APIService: `%s` — %s\n", check.APIService, apiStatus)
			if check.APIServiceMessage != "" {
				out = fmt.Appendf(out, "   - Message: %s\n", check.APIServiceMessage)
			}

			// Data status
			if !check.DataAvailable {
				out = fmt.Appendf(out, "   - Data: not available")
				if check.DataMessage != "" {
					out = fmt.Appendf(out, " (%s)", check.DataMessage)
				}
				out = append(out, '\n')
			}

			// Detail and remediation
			if check.Detail != "" {
				out = fmt.Appendf(out, "   - Detail: %s\n", check.Detail)
			}
			if check.Remediation != "" {
				out = fmt.Appendf(out, "   - Remediation: %s\n", check.Remediation)
			}

			if i < len(report.Checks)-1 {
				out = append(out, '\n')
			}
		}
	}

	_, err := w.Write(out)
	return err
}

// WriteMetricContractHTML writes a metrics contract report in HTML format.
func WriteMetricContractHTML(w io.Writer, report *MetricContractReport) error {
	var out []byte

	out = fmt.Appendf(out, `<h2>Metrics Contract: %s/%s (%s)</h2>`+"\n", report.Namespace, report.Name, report.Target)

	// Overall status
	statusClass := metricContractHTMLClass(report.OverallStatus)
	out = fmt.Appendf(out, `<p class="%s"><strong>Overall Status:</strong> %s</p>`+"\n", statusClass, report.OverallStatus)
	out = fmt.Appendf(out, "<p><strong>Summary:</strong> %s</p>"+"\n\n", report.Summary)

	if len(report.Remediation) > 0 {
		out = append(out, "<h3>Remediation</h3>"+"\n"...)
		out = append(out, "<ul>"+"\n"...)
		for _, step := range report.Remediation {
			out = fmt.Appendf(out, "  <li>%s</li>"+"\n", htmlEscape(step))
		}
		out = append(out, "</ul>"+"\n\n"...)
	}

	// Individual checks
	if len(report.Checks) > 0 {
		out = append(out, "<h3>Checks</h3>"+"\n"...)
		out = append(out, "<ol>"+"\n"...)
		for _, check := range report.Checks {
			checkClass := metricContractHTMLClass(check.Status)
			out = fmt.Appendf(out, `  <li class="%s"><strong>%s/%s</strong> — %s`+"\n", checkClass, check.MetricType, check.MetricName, check.Status)

			// APIService status
			apiStatus := "Available"
			if !check.APIServiceAvailable {
				apiStatus = "Unavailable"
			}
			out = fmt.Appendf(out, `    <ul><li>APIService: <code>%s</code> — %s</li>`+"\n", check.APIService, apiStatus)
			if check.APIServiceMessage != "" {
				out = fmt.Appendf(out, `    <li>Message: %s</li>`+"\n", metricContractHTMLEscape(check.APIServiceMessage))
			}

			// Data status
			if !check.DataAvailable {
				dataMsg := "not available"
				if check.DataMessage != "" {
					dataMsg += " (" + check.DataMessage + ")"
				}
				out = fmt.Appendf(out, `    <li>Data: %s</li>`+"\n", metricContractHTMLEscape(dataMsg))
			}

			// Detail and remediation
			if check.Detail != "" {
				out = fmt.Appendf(out, `    <li>Detail: %s</li>`+"\n", metricContractHTMLEscape(check.Detail))
			}
			if check.Remediation != "" {
				out = fmt.Appendf(out, `    <li>Remediation: %s</li>`+"\n", metricContractHTMLEscape(check.Remediation))
			}

			out = append(out, "    </ul></li>"+"\n"...)
		}
		out = append(out, "</ol>"+"\n"...)
	}

	_, err := w.Write(out)
	return err
}

// metricContractStatus returns a themed display string for the overall status.
func metricContractStatus(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("healthy")
	case "degraded":
		return theme.Bold.Render("degraded")
	case "broken":
		return theme.Error.Render("broken")
	default:
		return theme.Dim.Render(status)
	}
}

// metricContractIndicator returns a themed status indicator for a single check.
func metricContractIndicator(status string, theme style.Theme) string {
	switch status {
	case "ok":
		return theme.OK.Render("✓")
	case "missing-api":
		return theme.Error.Render("✗")
	case "missing-data", "selector-mismatch":
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// metricContractHTMLClass returns a CSS class for a status value in HTML output.
func metricContractHTMLClass(status string) string {
	switch status {
	case "healthy", "ok":
		return "status-healthy"
	case "degraded", "missing-data", "selector-mismatch":
		return "status-degraded"
	case "broken", "missing-api":
		return "status-broken"
	default:
		return "status-unknown"
	}
}

// metricContractHTMLEscape escapes special HTML characters for metric contract output.
func metricContractHTMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
