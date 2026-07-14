package rendutil

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// TruncateDisplayWidth truncates text to a terminal display width while
// preserving UTF-8 and ANSI escape sequences. tail is included in maxWidth.
func TruncateDisplayWidth(value string, maxWidth int, tail string) string {
	if maxWidth <= 0 {
		return ""
	}
	if ansi.StringWidth(value) <= maxWidth {
		return value
	}
	if ansi.StringWidth(tail) >= maxWidth {
		tail = ""
	}
	return ansi.Truncate(value, maxWidth, tail)
}

// PadDisplayWidth pads values shorter than width without discarding content.
// Wide runes and ANSI escape sequences are handled safely.
func PadDisplayWidth(value string, width int) string {
	padding := width - ansi.StringWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

// FitDisplayWidth truncates and then pads a value to exactly width columns.
func FitDisplayWidth(value string, width int) string {
	return PadDisplayWidth(TruncateDisplayWidth(value, width, ""), width)
}
