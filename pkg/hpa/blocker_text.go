package hpa

import (
	"fmt"
	"io"

	"charm.land/lipgloss/v2"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/blocker"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// AppendBlockerText writes the blocker analysis section to out. It renders the
// summary, blocker findings with severity badges, interpretation, and suggested
// next commands.
func AppendBlockerText(out *[]byte, report *blocker.Report, theme style.Theme, lbls labels) {
	if report == nil {
		return
	}

	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", lbls.Blockers)

	// Summary
	*out = append(*out, "  Summary:\n"...)
	*out = fmt.Appendf(*out, "    %s\n", report.Summary)

	// Blockers
	if len(report.Blockers) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, "  Scale-out blockers:\n"...)
		for _, b := range report.Blockers {
			badge := severityBadge(b.Severity, theme)
			*out = fmt.Appendf(*out, "    %s %s\n", badge, b.Message)
			if b.Detail != "" {
				*out = fmt.Appendf(*out, "      %s\n", theme.Dim.Render(b.Detail))
			}
		}
	} else {
		*out = append(*out, '\n')
		*out = append(*out, "  No scale-out blockers detected.\n"...)
	}

	// Interpretation
	if report.Interpretation != "" {
		*out = append(*out, '\n')
		*out = fmt.Appendf(*out, "  %s:\n", lbls.Interpretation)
		// Wrap long interpretation lines at ~80 chars for readability.
		for _, line := range wrapLines(report.Interpretation, 76) {
			*out = fmt.Appendf(*out, "    %s\n", theme.InterpretationLine(line))
		}
	}

	// Next commands
	if len(report.NextCommands) > 0 {
		*out = append(*out, '\n')
		*out = fmt.Appendf(*out, "  %s:\n", lbls.NextCommands)
		for _, cmd := range report.NextCommands {
			*out = fmt.Appendf(*out, "    - %s\n", cmd)
		}
	}
}

// WriteBlockerText writes a standalone blocker report (used by the blockers
// subcommand) with an HPA header line.
func WriteBlockerText(w io.Writer, report *blocker.Report, theme style.Theme) error {
	if report == nil {
		return nil
	}

	var out []byte
	out = fmt.Appendf(out, "HPA %s/%s\n\n", report.Namespace, report.Name)
	out = fmt.Appendf(out, "Target: %s\n", report.Target)
	out = fmt.Appendf(out, "Desired: %d  Ready: %d  Scale-out wanted: %v\n",
		report.DesiredReplicas, report.ReadyReplicas, report.HPAWantsScale)

	lbls := defaultBlockerLabels()
	AppendBlockerText(&out, report, theme, lbls)

	_, err := w.Write(out)
	return err
}

// severityBadge returns a styled severity label like [HIGH] or [INFO].
func severityBadge(severity blocker.Severity, theme style.Theme) string {
	var style lipgloss.Style
	switch severity {
	case blocker.BlockerHigh:
		style = theme.Error
	case blocker.BlockerMedium:
		style = theme.Warning
	case blocker.BlockerInfo:
		style = theme.Dim
	}
	return style.Render(fmt.Sprintf("[%s]", string(severity)))
}

// defaultBlockerLabels returns English labels for standalone blocker text output.
func defaultBlockerLabels() labels {
	return labels{
		Blockers:       "Scale-out blockers",
		Interpretation: "Interpretation",
		NextCommands:   "Next commands",
	}
}
