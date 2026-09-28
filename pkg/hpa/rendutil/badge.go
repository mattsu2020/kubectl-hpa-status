package rendutil

import "github.com/mattsu2020/kubectl-hpa-status/pkg/style"

// SeverityBadge renders a styled severity badge for the lowercase severity
// vocabulary shared by gitops review findings, autoscaler-map blockers, and
// scale-out blocker findings. "info" renders dimmed to match the historical
// blocker renderer; unknown levels fall back to an unstyled "[INFO]" badge so
// new severities remain readable instead of vanishing.
func SeverityBadge(level string, theme style.Theme) string {
	switch level {
	case "high":
		return theme.Error.Render("[HIGH]")
	case "medium":
		return theme.Warning.Render("[MED]")
	case "low":
		return theme.Dim.Render("[LOW]")
	case "info":
		return theme.Dim.Render("[INFO]")
	default:
		return "[INFO]"
	}
}
