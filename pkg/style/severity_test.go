package style

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestClassifySeverity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want SeverityTier
	}{
		// audit / lint vocabulary
		{"critical", SeverityCritical},
		{"error", SeverityCritical},
		{"warning", SeverityWarning},
		{"info", SeverityInfo},
		// blocker vocabulary (uppercase, no error level)
		{"HIGH", SeverityCritical},
		{"MEDIUM", SeverityWarning},
		{"INFO", SeverityInfo},
		// gitops vocabulary
		{"conflict", SeverityCritical},
		// replay / churn vocabulary
		{"high", SeverityCritical},
		{"medium", SeverityWarning},
		{"low", SeverityInfo},
		// tolerance and unknowns
		{"  Warning  ", SeverityWarning},
		{"", SeverityInfo},
		{"sev1", SeverityInfo},
	}
	for _, tc := range cases {
		if got := ClassifySeverity(tc.in); got != tc.want {
			t.Errorf("ClassifySeverity(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSeverityStyle(t *testing.T) {
	t.Parallel()
	theme := NewTheme(true)
	cases := []struct {
		tier SeverityTier
		want lipgloss.Style
	}{
		{SeverityCritical, theme.Error},
		{SeverityWarning, theme.Warning},
		{SeverityInfo, theme.Dim},
	}
	for _, tc := range cases {
		if got, want := theme.SeverityStyle(tc.tier).Render("x"), tc.want.Render("x"); got != want {
			t.Errorf("SeverityStyle(%d) rendered %q, want %q", tc.tier, got, want)
		}
	}
}
