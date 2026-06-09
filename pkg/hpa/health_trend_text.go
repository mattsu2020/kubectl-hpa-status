package hpa

import (
	"fmt"
	"strings"
)

// FormatTrendText renders a health trend summary for text output.
func FormatTrendText(result HealthTrendResult) string {
	if len(result.Snapshots) == 0 {
		return ""
	}

	var parts []string

	parts = append(parts, fmt.Sprintf("mean=%d min=%d max=%d",
		int(result.MeanScore), result.MinScore, result.MaxScore))

	if result.Variance > 0 {
		parts = append(parts, fmt.Sprintf("variance=%.1f", result.Variance))
	}

	if result.DegradationRate != 0 {
		direction := "improving"
		if result.DegradationRate < 0 {
			direction = "degrading"
		}
		parts = append(parts, fmt.Sprintf("%s (%.1f score/hour)", direction, result.DegradationRate))
	}

	if result.FlappingDetected {
		parts = append(parts, fmt.Sprintf("FLAPPING(%s)", result.FlappingSeverity))
	}

	if result.Sparkline != "" {
		parts = append(parts, result.Sparkline)
	}

	return "Health Trend: " + strings.Join(parts, " ")
}

// FormatTrendListRow renders a compact trend indicator for list view.
func FormatTrendListRow(result HealthTrendResult) string {
	if len(result.Snapshots) == 0 {
		return ""
	}

	parts := make([]string, 0, 3)

	if result.Sparkline != "" {
		parts = append(parts, result.Sparkline)
	}

	if result.DegradationRate < -5 {
		parts = append(parts, "down")
	} else if result.DegradationRate > 5 {
		parts = append(parts, "up")
	}

	if result.FlappingDetected {
		parts = append(parts, fmt.Sprintf("FLAP:%s", result.FlappingSeverity))
	}

	return strings.Join(parts, " ")
}
