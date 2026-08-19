package style

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// SeverityTier is the canonical severity classification shared across the
// domain-specific severity vocabularies (audit "critical/warning/info",
// blocker "HIGH/MEDIUM/INFO", lint "error/warning/info", gitops
// "conflict/warning/info", replay/churn "high/medium/low", ...). Domain enums
// stay wire-stable; this tier exists only for presentation decisions such as
// color and ordering, so a renderer never has to match two vocabularies at
// once.
type SeverityTier int

const (
	// SeverityInfo is the lowest tier: informational findings, low churn.
	SeverityInfo SeverityTier = iota
	// SeverityWarning covers "warning", "medium", and equivalent levels.
	SeverityWarning
	// SeverityCritical covers "critical", "error", "high", "conflict", and
	// equivalent levels.
	SeverityCritical
)

// ClassifySeverity maps a domain severity string to the canonical tier. The
// comparison is case-insensitive and ignores surrounding whitespace, so the
// uppercase blocker vocabulary and the lowercase replay vocabulary both
// resolve. Unknown values classify as SeverityInfo so a future vocabulary
// degrades gracefully instead of rendering as an error.
func ClassifySeverity(severity string) SeverityTier {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "error", "high", "conflict":
		return SeverityCritical
	case "warning", "medium":
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// SeverityStyle returns the theme style for a canonical severity tier:
// Error for critical, Warning for warning, Dim for informational.
func (t Theme) SeverityStyle(tier SeverityTier) lipgloss.Style {
	switch tier {
	case SeverityCritical:
		return t.Error
	case SeverityWarning:
		return t.Warning
	default:
		return t.Dim
	}
}
