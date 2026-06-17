package hpa

import (
	"fmt"
	"math"
	"strings"
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

// wrapLines splits text into lines of at most maxLen characters, breaking at
// word boundaries when possible. Shared by blocker, capacity-plan,
// gitops-review, and rollout text renderers.
func wrapLines(text string, maxLen int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	var current strings.Builder

	for _, word := range words {
		if current.Len() > 0 && current.Len()+1+len(word) > maxLen {
			lines = append(lines, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
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
