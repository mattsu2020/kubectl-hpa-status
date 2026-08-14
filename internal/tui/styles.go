package tui

import (
	"charm.land/lipgloss/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// Styles for the TUI.
var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	errorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	warnStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func healthStyle(health string) lipgloss.Style {
	switch health {
	case string(hpaanalysis.HealthOK):
		return okStyle
	case string(hpaanalysis.HealthError):
		return errorStyle
	default:
		return warnStyle
	}
}

func truncate(s string, maxLen int) string {
	return rendutil.TruncateDisplayWidth(s, maxLen, "…")
}

func padRight(s string, width int) string {
	return rendutil.FitDisplayWidth(s, width)
}
