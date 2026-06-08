package hpa

import (
	"fmt"
	"math"
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
