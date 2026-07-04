// Package style provides centralized terminal styling definitions for
// kubectl-hpa-status. When color is disabled all styles produce plain text.
package style

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Theme holds all styled renderers used by the CLI output layer.
type Theme struct {
	// Health states
	OK      lipgloss.Style
	Error   lipgloss.Style
	Limited lipgloss.Style

	// Structural
	Header lipgloss.Style
	Dim    lipgloss.Style
	Bold   lipgloss.Style

	// Semantic (used in status output)
	Warning lipgloss.Style
	Success lipgloss.Style

	// enabled tracks whether color is active so callers can branch if needed.
	enabled bool
}

// NewTheme creates a Theme. When colorEnabled is false all styles render
// plain strings with no ANSI escape codes.
func NewTheme(colorEnabled bool) Theme {
	if !colorEnabled {
		return Theme{enabled: false}
	}

	// lipgloss v2 downsamples colors at write time via the global
	// lipgloss.Writer (a colorprofile.Writer). Pin its Profile to TrueColor so
	// styles emit full-fidelity ANSI codes even when stdout is not a terminal
	// (important for tests and piped output with --color=always). This replaces
	// the v1 pattern of constructing a renderer with an explicit profile.
	lipgloss.Writer.Profile = colorprofile.TrueColor

	return Theme{
		enabled: true,
		OK:      lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		Limited: lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
		Header:  lipgloss.NewStyle().Bold(true),
		Dim:     lipgloss.NewStyle().Faint(true),
		Bold:    lipgloss.NewStyle().Bold(true),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	}
}

// Enabled returns true when the theme produces ANSI-styled output.
func (t Theme) Enabled() bool { return t.enabled }

// HealthLabel renders a health status string (OK, ERROR, LIMITED) with
// appropriate styling, prefix markers, and a score-based tier indicator.
// Tier: 90+ = Excellent, 70-89 = Warning, <70 = Critical.
func (t Theme) HealthLabel(health string, score int) string {
	tier := "Critical"
	switch {
	case score >= 90:
		tier = "Excellent"
	case score >= 70:
		tier = "Warning"
	}

	label := health
	switch health {
	case "ERROR":
		label = fmt.Sprintf("🔴 ERROR (%s)", tier)
	case "LIMITED":
		label = fmt.Sprintf("🔴 ScalingLimited (%s)", tier)
	case "STABILIZED":
		label = fmt.Sprintf("🟡 Stabilized (%s)", tier)
	case "OK":
		label = fmt.Sprintf("🟢 Healthy (%s)", tier)
	}
	switch health {
	case "ERROR":
		return t.Error.Render(label)
	case "LIMITED":
		return t.Limited.Render(label)
	case "STABILIZED":
		return t.Warning.Render(label)
	case "OK":
		return t.OK.Render(label)
	default:
		return label
	}
}

// Issue renders an issue string colored by its health severity.
func (t Theme) Issue(issue, health string) string {
	if !t.enabled || issue == "" {
		return issue
	}
	switch health {
	case "ERROR":
		return t.Error.Render(issue)
	case "LIMITED":
		return t.Limited.Render(issue)
	default:
		return issue
	}
}

// ConditionStatus renders a condition status string with color indicating
// whether the condition is healthy for the given condition type.
func (t Theme) ConditionStatus(conditionType, status string) string {
	if !t.enabled {
		return status
	}
	switch conditionType {
	case "ScalingActive":
		if status != "True" {
			return t.Error.Render(status)
		}
		return t.Success.Render(status)
	case "ScalingLimited":
		if status == "True" {
			return t.Warning.Render(status)
		}
		return t.Success.Render(status)
	default:
		return status
	}
}

// SummaryColor renders the summary text with color based on content.
func (t Theme) SummaryColor(summary string) string {
	if !t.enabled {
		return summary
	}
	if containsAny(summary, "cannot currently compute", "no visible desired") {
		return t.Error.Render(summary)
	}
	if containsAny(summary, "scale up", "wants to scale") {
		return t.Warning.Render(summary)
	}
	if containsAny(summary, "maxReplicas", "minReplicas") {
		return t.Warning.Render(summary)
	}
	return summary
}

// MetricNote renders a metric comparison note with color.
func (t Theme) MetricNote(note string) string {
	if !t.enabled || note == "" {
		return note
	}
	if containsAny(note, "above target") {
		return t.Error.Render(note)
	}
	if containsAny(note, "below target") {
		return t.Success.Render(note)
	}
	return note
}

// InterpretationLine renders an interpretation line, dimming estimated and unknown findings.
func (t Theme) InterpretationLine(line string) string {
	if !t.enabled {
		return line
	}
	if containsAny(line, "[estimated]") || containsAny(line, "[unknown]") {
		return t.Dim.Render(line)
	}
	return line
}

// ActionLine renders a recommended action line with warning color.
func (t Theme) ActionLine(line string) string {
	if !t.enabled {
		return line
	}
	return t.Warning.Render(line)
}

// ReplicaHighlight renders a replica count with emphasis when it differs from expected.
func (t Theme) ReplicaHighlight(value int32, highlight bool) string {
	text := fmt.Sprintf("%d", value)
	if !t.enabled || !highlight {
		return text
	}
	return t.Bold.Render(text)
}

// ScreenClear returns the ANSI escape sequence to clear the terminal screen
// and move the cursor to the top-left. Returns empty string when color is
// disabled (i.e., output is piped).
func (t Theme) ScreenClear() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[2J\x1b[H"
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
