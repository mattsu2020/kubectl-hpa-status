package rendutil

import (
	"fmt"
	"strings"
	"time"
)

// DurationCompactMinutes formats a duration rounded to whole minutes in the
// compact "1h30m" / "45m" shape shared by the retrospective and flapping
// reports. Durations under half a minute round to "0m".
func DurationCompactMinutes(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// DurationSpaced formats a duration using spaces between units. It keeps the
// two most useful units and is intended for prose such as "4m 12s".
func DurationSpaced(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	seconds := int64(d / time.Second)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// DurationCompact formats a duration without spaces, using at most two units.
// Negative durations are treated as elapsed magnitudes.
func DurationCompact(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	seconds := int64(d / time.Second)
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0 && secs > 0:
		return fmt.Sprintf("%dm%ds", minutes, secs)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// DurationCompactHMS formats seconds, minutes, and hours without converting
// hours into days. It preserves long-running freshness output such as 25h3m.
func DurationCompactHMS(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	seconds := int64(d / time.Second)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// ProgressBar renders a clamped filled/empty Unicode bar.
func ProgressBar(value, total, width int64) string {
	return progressBar(value, total, width, true)
}

// ProgressBarFloor renders a bar using integer truncation. It is used where a
// full bar must mean the value actually reached the total (for example 100%).
func ProgressBarFloor(value, total, width int64) string {
	return progressBar(value, total, width, false)
}

func progressBar(value, total, width int64, round bool) string {
	if width <= 0 {
		return ""
	}
	filled := int64(0)
	if total > 0 && value > 0 {
		filled = value * width
		if round {
			filled += total / 2
		}
		filled /= total
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", int(filled)) + strings.Repeat("░", int(width-filled))
}
