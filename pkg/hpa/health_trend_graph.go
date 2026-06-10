package hpa

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	// graphYSteps is the number of horizontal rows in the graph.
	graphYSteps = 5

	// graphAnomalyWindow is the number of snapshots around a detected anomaly
	// that are marked with the anomaly character.
	graphAnomalyWindow = 1
)

// RenderHealthTrendASCII renders a horizontal ASCII time-series graph of
// health scores. X-axis represents time and Y-axis represents the health
// score (0-100). The width parameter controls the graph width in characters.
// Returns an empty string if there are no snapshots or width <= 0.
func RenderHealthTrendASCII(snapshots []HealthSnapshot, width int) string {
	if len(snapshots) == 0 || width <= 0 {
		return ""
	}

	// Use a working copy to avoid mutating the input.
	sorted := make([]HealthSnapshot, len(snapshots))
	copy(sorted, snapshots)

	// Detect anomalies to mark on the graph.
	anomalies := DetectAnomalies(sorted)
	anomalyIndices := buildAnomalyIndexSet(sorted, anomalies)

	// Map snapshots to column positions.
	columns := mapSnapshotsToColumns(sorted, width)

	// Build graph rows from top (100) to bottom (0).
	rows := make([]string, graphYSteps+1)
	for row := 0; row < graphYSteps; row++ {
		yMax := 100 - row*(100/graphYSteps)
		rows[row] = renderGraphRow(sorted, columns, anomalyIndices, yMax, width)
	}

	// Build x-axis.
	rows[graphYSteps] = renderXAxis(sorted, columns, width)

	return strings.Join(rows, "\n") + "\n"
}

// mapSnapshotsToColumns assigns each snapshot an x-axis column position.
func mapSnapshotsToColumns(snapshots []HealthSnapshot, width int) []int {
	columns := make([]int, len(snapshots))
	if len(snapshots) == 1 {
		columns[0] = width / 2
		return columns
	}

	for i := range snapshots {
		ratio := float64(i) / float64(len(snapshots)-1)
		columns[i] = int(math.Round(ratio * float64(width)))
		if columns[i] >= width {
			columns[i] = width - 1
		}
	}

	return columns
}

// renderGraphRow renders a single horizontal row of the graph at the given
// y-axis maximum value.
func renderGraphRow(snapshots []HealthSnapshot, columns []int, anomalyIndices map[int]bool, yMax int, width int) string {
	threshold := yMax - 100/graphYSteps
	if threshold < 0 {
		threshold = 0
	}

	line := make([]rune, width)
	for i := range line {
		line[i] = ' '
	}

	// Place data points that fall within this row's score band.
	for idx, snapshot := range snapshots {
		col := columns[idx]
		if col < 0 || col >= width {
			continue
		}

		score := snapshot.HealthScore
		if score > threshold && score <= yMax {
			if anomalyIndices[idx] {
				line[col] = '╳'
			} else {
				line[col] = '•'
			}
		}
	}

	// Connect adjacent data points with horizontal lines.
	connectAdjacentPoints(line, snapshots, columns, threshold, yMax, width)

	label := fmt.Sprintf("%3d", yMax)
	return label + " │" + string(line)
}

// connectAdjacentPoints draws horizontal connection lines between adjacent
// data points that fall within the same row's score band.
func connectAdjacentPoints(line []rune, snapshots []HealthSnapshot, columns []int, threshold int, yMax int, width int) {
	for i := 0; i < len(snapshots)-1; i++ {
		scoreI := snapshots[i].HealthScore
		scoreJ := snapshots[i+1].HealthScore

		inBandI := scoreI > threshold && scoreI <= yMax
		inBandJ := scoreJ > threshold && scoreJ <= yMax

		if !inBandI && !inBandJ {
			continue
		}

		colI := columns[i]
		colJ := columns[i+1]

		if colJ-colI <= 1 {
			continue
		}

		// Fill the gap between two points in the same band.
		if inBandI && inBandJ {
			for c := colI + 1; c < colJ; c++ {
				if c >= 0 && c < width && line[c] == ' ' {
					line[c] = '─'
				}
			}
		}
	}
}

// renderXAxis renders the bottom x-axis line with timestamp labels.
func renderXAxis(snapshots []HealthSnapshot, columns []int, width int) string {
	axisRunes := make([]rune, width)
	for i := range axisRunes {
		axisRunes[i] = '─'
	}

	axisLine := "  0 └" + string(axisRunes)

	// Build timestamp labels below the axis.
	labelLine := "    "
	labelRunes := make([]rune, width)
	for i := range labelRunes {
		labelRunes[i] = ' '
	}

	// Place timestamps at evenly spaced positions.
	maxLabels := width / 12
	if maxLabels < 2 {
		maxLabels = 2
	}
	if maxLabels > 6 {
		maxLabels = 6
	}

	indicesToLabel := evenlySpacedIndices(len(snapshots), maxLabels)
	for _, idx := range indicesToLabel {
		col := columns[idx]
		ts := snapshots[idx].Timestamp.Format("15:04")
		tsRunes := []rune(ts)
		start := col - len(tsRunes)/2
		if start < 0 {
			start = 0
		}
		if start+len(tsRunes) > width {
			start = width - len(tsRunes)
		}
		for j, r := range tsRunes {
			pos := start + j
			if pos >= 0 && pos < width {
				labelRunes[pos] = r
			}
		}
	}

	labelLine += string(labelRunes)

	return axisLine + "\n" + labelLine
}

// evenlySpacedIndices returns up to count indices evenly distributed across
// the range [0, n-1].
func evenlySpacedIndices(n int, count int) []int {
	if n <= count {
		result := make([]int, n)
		for i := 0; i < n; i++ {
			result[i] = i
		}
		return result
	}

	result := make([]int, count)
	for i := 0; i < count; i++ {
		result[i] = int(float64(i) * float64(n-1) / float64(count-1))
	}
	return result
}

// buildAnomalyIndexSet creates a set of snapshot indices that are near
// detected anomalies, so the graph can render them with anomaly markers.
func buildAnomalyIndexSet(snapshots []HealthSnapshot, anomalies []AnomalyDetection) map[int]bool {
	iface := make(map[int]bool)

	for _, anomaly := range anomalies {
		for idx, snapshot := range snapshots {
			diff := snapshot.Timestamp.Sub(anomaly.Timestamp)
			if diff < 0 {
				diff = -diff
			}
			if diff <= time.Minute {
				for delta := -graphAnomalyWindow; delta <= graphAnomalyWindow; delta++ {
					pos := idx + delta
					if pos >= 0 && pos < len(snapshots) {
						iface[pos] = true
					}
				}
			}
		}
	}

	return iface
}
