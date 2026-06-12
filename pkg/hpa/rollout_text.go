package hpa

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

// AppendRolloutReportText appends the rollout report section to out. It renders
// the summary, checks with pass/fail indicators, risks with severity badges,
// recommendation, and next actions.
func AppendRolloutReportText(out *[]byte, report *RolloutReport, theme style.Theme) {
	if report == nil {
		return
	}

	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "Rollout checks:\n")

	// Summary.
	*out = append(*out, "  Summary:\n"...)
	*out = fmt.Appendf(*out, "    %s\n", report.Summary)

	// Checks.
	if len(report.Checks) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, "  Checks:\n"...)
		for _, c := range report.Checks {
			indicator := theme.OK.Render("PASS")
			if !c.Pass {
				indicator = theme.Error.Render("FAIL")
			}
			*out = fmt.Appendf(*out, "    %s [%s] %s\n", indicator, c.Category, c.Message)
		}
	}

	// Risks.
	if len(report.Risks) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, "  Risks:\n"...)
		for _, r := range report.Risks {
			badge := rolloutRiskBadge(r.Severity, theme)
			*out = fmt.Appendf(*out, "    %s %s: %s\n", badge, r.Category, r.Message)
			if r.Detail != "" {
				for _, line := range wrapLines(r.Detail, 72) {
					*out = fmt.Appendf(*out, "      %s\n", line)
				}
			}
		}
	}

	// Recommendation.
	if report.Recommendation != "" {
		*out = append(*out, '\n')
		*out = append(*out, "  Recommendation:\n"...)
		for _, line := range wrapLines(report.Recommendation, 76) {
			*out = fmt.Appendf(*out, "    %s\n", line)
		}
	}

	// Next actions.
	if len(report.NextActions) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, "  Next actions:\n"...)
		for _, action := range report.NextActions {
			*out = fmt.Appendf(*out, "    - %s\n", action)
		}
	}
}

// WriteRolloutReportText writes a standalone rollout report (used by the rollout
// subcommand) with a header.
func WriteRolloutReportText(w io.Writer, report *RolloutReport, theme style.Theme) error {
	if report == nil {
		return nil
	}

	var out []byte
	out = fmt.Appendf(out, "Rollout-aware HPA checks for HPA %s/%s\n", report.Name, report.Namespace)
	out = fmt.Appendf(out, "Target: %s\n", report.Target)
	out = fmt.Appendf(out, "Rollout in progress: %s\n", rolloutBoolStr(report.RolloutInProgress))
	if report.RolloutInProgress && report.NewPodsReady != "" {
		out = fmt.Appendf(out, "New pods ready: %s\n", report.NewPodsReady)
	}

	AppendRolloutReportText(&out, report, theme)

	_, err := w.Write(out)
	return err
}

// rolloutRiskBadge returns a styled severity badge for rollout risks.
func rolloutRiskBadge(severity string, theme style.Theme) string {
	var style lipgloss.Style
	switch severity {
	case "high":
		style = theme.Error
	case "medium":
		style = theme.Warning
	case "low":
		style = theme.Dim
	default:
		return "[INFO]"
	}
	return style.Render(fmt.Sprintf("[%s]", severityLabel(severity)))
}

// severityLabel returns an uppercase abbreviated label for the severity.
func severityLabel(severity string) string {
	switch severity {
	case "high":
		return "HIGH"
	case "medium":
		return "MED"
	case "low":
		return "LOW"
	default:
		return "INFO"
	}
}

// rolloutBoolStr returns "yes" or "no" for a boolean.
func rolloutBoolStr(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
