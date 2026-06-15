package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// WriteGitOpsReviewText writes a standalone GitOps review report.
func WriteGitOpsReviewText(w io.Writer, review *GitOpsReview, theme style.Theme) error {
	if review == nil {
		return nil
	}

	var buf strings.Builder

	buf.WriteString("HPA risk review:\n\n")

	if len(review.Files) == 0 {
		buf.WriteString("No HPA manifests found.\n")
		_, err := w.Write([]byte(buf.String()))
		return err
	}

	for _, file := range review.Files {
		if len(file.Findings) == 0 {
			buf.WriteString(fmt.Sprintf("  %s: no issues\n", file.Path))
			continue
		}

		header := file.Path
		if file.HPAName != "" {
			header = fmt.Sprintf("%s (%s)", file.Path, file.HPAName)
		}
		buf.WriteString(fmt.Sprintf("  %s:\n", header))

		for _, f := range file.Findings {
			badge := reviewSeverityBadge(f.Severity, theme)
			buf.WriteString(fmt.Sprintf("    %s [%s] %s\n", badge, f.Category, f.Message))
			if f.Detail != "" {
				for _, line := range wrapLines(f.Detail, 72) {
					buf.WriteString(fmt.Sprintf("      %s\n", line))
				}
			}
		}
		buf.WriteString("\n")
	}

	// Summary.
	buf.WriteString(fmt.Sprintf("Summary: %s\n", review.Summary))
	buf.WriteString(fmt.Sprintf("Risk level: %s\n", reviewRiskLabel(review.RiskLevel, theme)))

	// Recommendation.
	if review.Recommendation != "" {
		buf.WriteString("\nRecommendation:\n")
		for _, line := range wrapLines(review.Recommendation, 76) {
			buf.WriteString(fmt.Sprintf("  %s\n", line))
		}
	}

	_, err := w.Write([]byte(buf.String()))
	return err
}

// reviewSeverityBadge returns a styled severity badge.
func reviewSeverityBadge(severity string, theme style.Theme) string {
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

// reviewRiskLabel returns a styled risk level label.
func reviewRiskLabel(level string, theme style.Theme) string {
	switch level {
	case "high":
		return theme.Error.Render(level)
	case "medium":
		return theme.Warning.Render(level)
	case "low":
		return theme.Dim.Render(level)
	default:
		return level
	}
}
