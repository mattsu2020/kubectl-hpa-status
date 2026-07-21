package hpa

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// FormatDuration returns a human-readable duration string from seconds.
// Examples: "0s", "45s", "4m 12s", "1h 23m", "2h 0m".
func FormatDuration(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// FormatStabilizationRemaining returns a human-readable string like
// "4m 12s" from a remaining-seconds value. Returns "" if remaining
// is nil or <= 0.
func FormatStabilizationRemaining(remaining *int64) string {
	if remaining == nil || *remaining <= 0 {
		return ""
	}
	return FormatDuration(*remaining)
}

// FormatStabilizationProgress returns a progress description like
// "4m 12s remaining (of 5m 0s)" with the window context.
// Returns "" if data is insufficient.
func FormatStabilizationProgress(remaining *int64, windowSeconds *int32) string {
	if remaining == nil || *remaining <= 0 {
		return ""
	}
	r := FormatDuration(*remaining)
	if windowSeconds == nil || *windowSeconds <= 0 {
		return r + " remaining"
	}
	w := FormatDuration(int64(*windowSeconds))
	return r + " remaining (of " + w + ")"
}

// StabilizationProgressRatio returns 0.0–1.0 representing how much
// of the stabilization window has elapsed. Returns 0 if data is
// insufficient. A higher ratio means more time has elapsed (closer
// to unstabilizing).
func StabilizationProgressRatio(remaining *int64, windowSeconds *int32) float64 {
	if remaining == nil || windowSeconds == nil || *windowSeconds <= 0 {
		return 0
	}
	elapsed := float64(*windowSeconds) - float64(*remaining)
	ratio := elapsed / float64(*windowSeconds)
	return math.Max(0, math.Min(1.0, ratio))
}

// FormatCountdownBadge returns a compact stabilization badge like
// "⏳ 4m12s" for display in tables. Returns "" if not stabilizing.
func FormatCountdownBadge(remaining *int64) string {
	if remaining == nil || *remaining <= 0 {
		return ""
	}
	s := *remaining
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	switch {
	case h > 0:
		return fmt.Sprintf("⏳ %dh%dm", h, m)
	case m > 0:
		return fmt.Sprintf("⏳ %dm%ds", m, sec)
	default:
		return fmt.Sprintf("⏳ %ds", sec)
	}
}

// progressBar renders a 10-cell unicode bar for a metric ratio (0–2 mapped to
// 0–10 filled cells). Shared by the list and status text renderers.
func progressBar(ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 2 {
		ratio = 2
	}
	filled := int((ratio/2)*10 + 0.5)
	if filled > 10 {
		filled = 10
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

// formatMetricText reconstructs a metric display line using the original Text
// but with the note replaced by a potentially colorized version. Shared by the
// diff and status-section text renderers.
func formatMetricText(m Metric, coloredNote string) string {
	if m.Note == "" || coloredNote == m.Note {
		return m.Text
	}
	return strings.Replace(m.Text, m.Note, coloredNote, 1)
}

// --- Text-rendering helpers shared between text.go and text_sections.go ---
//
// These badge/indicator/duration helpers were originally defined in text.go
// and called only from text_sections.go. Moving them here keeps text.go as
// the orchestrator and makes the shared format surface explicit, reducing
// the coupling that blocks a future render subpackage.

// triggerStatusBadge returns a display string with a visual indicator for a KEDA trigger status.
func triggerStatusBadge(status string, theme style.Theme) string {
	switch status {
	case "Active":
		return theme.OK.Render("Active ✓")
	case "Inactive":
		return theme.Error.Render("Inactive ✗")
	default:
		return theme.Dim.Render("Unknown ?")
	}
}

// metricsDiagnosticsStatus returns a themed display string for the overall metrics diagnostics status.
func metricsDiagnosticsStatus(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("healthy")
	case "degraded":
		return theme.Bold.Render("degraded")
	case "error":
		return theme.Error.Render("error")
	default:
		return theme.Dim.Render(status)
	}
}

// metricsDiagnosticsIndicator returns a themed status indicator for a per-metric health check.
func metricsDiagnosticsIndicator(status string, theme style.Theme) string {
	switch status {
	case "healthy":
		return theme.OK.Render("✓")
	case "missing":
		return theme.Error.Render("✗")
	case "stale":
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// metricFreshnessIndicator returns a themed status indicator for a metric
// freshness entry.
func metricFreshnessIndicator(status string, theme style.Theme) string {
	switch status {
	case string(FreshnessOK):
		return theme.OK.Render("✓")
	case string(FreshnessMissing):
		return theme.Error.Render("✗")
	case string(FreshnessStale):
		return theme.Bold.Render("!")
	default:
		return theme.Dim.Render("?")
	}
}

// metricFreshnessStatusDisplay returns a themed display string for the
// freshness status label.
func metricFreshnessStatusDisplay(status string, theme style.Theme) string {
	switch status {
	case string(FreshnessOK):
		return theme.OK.Render(string(FreshnessOK))
	case string(FreshnessMissing):
		return theme.Error.Render(string(FreshnessMissing))
	case string(FreshnessStale):
		return theme.Bold.Render(string(FreshnessStale))
	default:
		return theme.Dim.Render("UNKNOWN")
	}
}

// formatFreshnessDuration returns a human-readable duration string
// (e.g., "12s", "4m32s", "1h5m").
func formatFreshnessDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int64(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	hours := minutes / 60
	mins := minutes % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// emptyAsUnknown returns "<unknown>" when value is the empty string.
func emptyAsUnknown(value string) string {
	if value == "" {
		return "<unknown>"
	}
	return value
}

// indentBlock prefixes every line of text with prefix and ensures a trailing
// newline.
func indentBlock(text string, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
