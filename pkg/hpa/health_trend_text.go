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

// FormatTrendAnomalyText renders anomaly detection results as text.
// Returns an empty string if no anomalies were detected.
func FormatTrendAnomalyText(result HealthTrendResult) string {
	if len(result.Anomalies) == 0 {
		return ""
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("Anomalies: %d detected", len(result.Anomalies)))

	for i, anomaly := range result.Anomalies {
		lines = append(lines, fmt.Sprintf("  [%d] %s (%s) score %d->%d duration=%s",
			i+1,
			anomaly.Type,
			anomaly.Severity,
			anomaly.ScoreBefore,
			anomaly.ScoreAfter,
			anomaly.Duration,
		))
		if anomaly.CauseEstimate != "" {
			lines = append(lines, fmt.Sprintf("      cause: %s", anomaly.CauseEstimate))
		}
		if anomaly.Remediation != "" {
			lines = append(lines, fmt.Sprintf("      fix:   %s", anomaly.Remediation))
		}
	}

	return strings.Join(lines, "\n")
}

// FormatTrendAnomalyGraph renders the full trend text plus anomaly section
// and ASCII graph. Returns an empty string if no snapshots are available.
func FormatTrendAnomalyGraph(result HealthTrendResult, graphWidth int) string {
	trendText := FormatTrendText(result)
	if trendText == "" {
		return ""
	}

	var sections []string
	sections = append(sections, trendText)

	anomalyText := FormatTrendAnomalyText(result)
	if anomalyText != "" {
		sections = append(sections, anomalyText)
	}

	graph := RenderHealthTrendASCII(result.Snapshots, graphWidth)
	if graph != "" {
		sections = append(sections, graph)
	}

	return strings.Join(sections, "\n\n")
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
