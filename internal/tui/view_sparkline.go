package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// sparklineBlocks maps normalized values 0-7 to Unicode block characters.
var sparklineBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// renderSparkline renders a Unicode sparkline from numeric values.
// Values are normalized to the 0-7 range and mapped to block characters.
func renderSparkline(values []float64, width int, style lipgloss.Style) string {
	if len(values) == 0 {
		return ""
	}
	if width <= 0 {
		width = len(values)
	}
	if width > len(values) {
		width = len(values)
	}

	if len(values) == 1 {
		return style.Render("█")
	}

	// Find min and max for normalization.
	minVal, maxVal := values[0], values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// All same values: render middle block repeated.
	if maxVal == minVal {
		result := strings.Repeat("▄", width)
		return style.Render(result)
	}

	// Use the last `width` values if we need to truncate.
	start := len(values) - width
	used := values[start:]

	// Normalize each value to 0-7 and map to block character.
	runes := make([]rune, len(used))
	rangeVal := maxVal - minVal
	for i, v := range used {
		normalized := (v - minVal) / rangeVal * 7.0
		idx := int(normalized)
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		runes[i] = sparklineBlocks[idx]
	}

	return style.Render(string(runes))
}

// renderSparklineWithMarkers renders a sparkline with direction-flip markers
// at specified indices. At marker positions, ↕ is rendered instead of a block.
func renderSparklineWithMarkers(values []float64, width int, markers map[int]bool, style lipgloss.Style) string {
	if len(values) == 0 {
		return ""
	}
	if width <= 0 {
		width = len(values)
	}
	if width > len(values) {
		width = len(values)
	}
	if len(values) == 1 {
		return style.Render("█")
	}

	minVal, maxVal := values[0], values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == minVal {
		return style.Render(strings.Repeat("▄", width))
	}

	start := len(values) - width
	used := values[start:]
	rangeVal := maxVal - minVal

	var sb strings.Builder
	for i, v := range used {
		absIdx := start + i
		if markers[absIdx] {
			sb.WriteString(errorStyle.Render("↕"))
		} else {
			normalized := (v - minVal) / rangeVal * 7.0
			idx := int(normalized)
			if idx < 0 {
				idx = 0
			}
			if idx > 7 {
				idx = 7
			}
			sb.WriteString(style.Render(string(sparklineBlocks[idx])))
		}
	}
	return sb.String()
}

// detectDirectionFlips returns the set of indices where replica values
// change direction (scale-up → scale-down or vice versa).
func detectDirectionFlips(values []float64) map[int]bool {
	flips := make(map[int]bool)
	if len(values) < 3 {
		return flips
	}
	prev := values[0]
	curr := values[1]
	for i := 2; i < len(values); i++ {
		next := values[i]
		prevDir := curr - prev
		nextDir := next - curr
		if (prevDir > 0 && nextDir < 0) || (prevDir < 0 && nextDir > 0) {
			flips[i-1] = true
		}
		prev = curr
		curr = next
	}
	return flips
}
