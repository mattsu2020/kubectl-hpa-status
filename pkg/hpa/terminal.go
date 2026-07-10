package hpa

import (
	"strings"
	"unicode"
)

// SanitizeTerminalText removes terminal control sequences from untrusted
// Kubernetes strings. JSON/YAML renderers keep the original value and rely on
// their encoders; text and TUI renderers should call this before interpolation.
func SanitizeTerminalText(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		case '\u007f':
			return -1
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
