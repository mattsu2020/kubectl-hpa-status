// Package enrichment provides KEDA and VPA enrichment logic for HPA analysis.
package enrichment

import "strings"

// Mode is the normalized tri-state enrichment policy.
type Mode uint8

const (
	// ModeOff disables enrichment.
	ModeOff Mode = iota
	// ModeAuto enables enrichment only when the CRD is discovered.
	ModeAuto
	// ModeOn forces enrichment.
	ModeOn
)

// ParseMode normalizes current and legacy flag spellings. Unknown values are
// disabled; CLI validation is responsible for presenting the user error.
func ParseMode(value string) Mode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1":
		return ModeOn
	case "auto":
		return ModeAuto
	default:
		return ModeOff
	}
}

// Requested reports whether discovery should be attempted.
func (m Mode) Requested() bool { return m != ModeOff }

// Enabled evaluates the mode against CRD availability.
func (m Mode) Enabled(crdPresent bool) bool {
	return m == ModeOn || (m == ModeAuto && crdPresent)
}

// Requested reports whether the mode asks for enrichment at all (on or auto),
// as opposed to off/empty which skip discovery entirely. It also accepts the
// legacy bool spellings ("true"/"1") so existing --keda=true invocations keep
// working after the flag became a tri-state string. It is exported so callers
// outside the package (e.g. cmd's streaming-eligibility check) share one
// definition instead of mirroring the switch.
func Requested(mode string) bool {
	return ParseMode(mode).Requested()
}
