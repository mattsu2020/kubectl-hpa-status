package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// historyState holds the history/sparkline view state for a single HPA.
type historyState struct {
	snapshots     []hpaanalysis.TimelineSnapshot
	churnAnalysis *hpaanalysis.ChurnAnalysis
	scrollPos     int
}

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

// renderHealthTimeline renders a single-line health timeline using colored
// characters. Each snapshot maps to a colored block based on its health.
func renderHealthTimeline(snapshots []hpaanalysis.TimelineSnapshot, width int) string {
	if len(snapshots) == 0 {
		return ""
	}

	used := snapshots
	if width > 0 && len(used) > width {
		used = used[len(used)-width:]
	}

	var sb strings.Builder
	for _, snap := range used {
		ch := "█"
		var s lipgloss.Style
		switch snap.Health {
		case string(hpaanalysis.HealthOK):
			s = okStyle
		case string(hpaanalysis.HealthLimited), string(hpaanalysis.HealthStabilized):
			ch = "▓"
			s = warnStyle
		case string(hpaanalysis.HealthError):
			ch = "░"
			s = errorStyle
		default:
			// Color by score for unknown health states.
			switch {
			case snap.HealthScore >= 80:
				s = okStyle
			case snap.HealthScore >= 50:
				s = warnStyle
			default:
				s = errorStyle
			}
		}
		sb.WriteString(s.Render(ch))
	}

	return sb.String()
}

// churnColor returns the appropriate style for a churn level.
func churnColor(level string) lipgloss.Style {
	switch level {
	case "LOW":
		return okStyle
	case "MEDIUM":
		return warnStyle
	case "HIGH":
		return errorStyle
	case "CRITICAL":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	default:
		return dimStyle
	}
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

// renderHistoryView renders the history/sparkline view for the selected HPA.
func (m Model) renderHistoryView() string {
	items := m.filteredItems()
	if m.cursor >= len(items) {
		return "No HPA selected"
	}

	item := items[m.cursor]

	// Determine available snapshots from history state.
	var snapshots []hpaanalysis.TimelineSnapshot
	var churn *hpaanalysis.ChurnAnalysis
	var scrollPos int

	if m.historyState != nil {
		hs := m.historyState
		snapshots = hs.snapshots
		churn = hs.churnAnalysis
		scrollPos = hs.scrollPos
	}

	if len(snapshots) == 0 {
		return renderHistoryEmpty(item)
	}

	// Derive churn analysis from snapshots if not already computed.
	if churn == nil {
		churn = hpaanalysis.AnalyzeChurnFromSnapshots(snapshots, nil)
	}

	var sb strings.Builder
	graphWidth := historyGraphWidth(m.width)

	appendHistoryHeader(&sb, item, snapshots)
	appendHistoryChurnSection(&sb, churn)
	appendHistoryRecommendations(&sb, churn, m.width)
	appendHistoryReplicaTrend(&sb, snapshots, churn, graphWidth)
	appendHistoryMetricTrends(&sb, m.reports[item.Namespace+"/"+item.Name])
	appendHistoryHealthTimeline(&sb, snapshots, graphWidth)
	appendHistoryEventLog(&sb, snapshots, scrollPos, m.height, m.width)

	// 7. Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("↑/k: scroll up | ↓/j: scroll down | esc: back"))

	return sb.String()
}

func renderHistoryEmpty(item hpaanalysis.ListItem) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(fmt.Sprintf("HPA History: %s/%s", item.Namespace, item.Name)))
	sb.WriteString("\n\n")
	sb.WriteString(dimStyle.Render("No timeline data available. Use 'timeline record' to capture data."))
	sb.WriteString("\n")
	return sb.String()
}

func historyGraphWidth(width int) int {
	graphWidth := width - 20
	if graphWidth < 10 {
		graphWidth = 10
	}
	return graphWidth
}

func appendHistoryHeader(sb *strings.Builder, item hpaanalysis.ListItem, snapshots []hpaanalysis.TimelineSnapshot) {
	sb.WriteString(headerStyle.Render(fmt.Sprintf("HPA History: %s/%s", item.Namespace, item.Name)))
	sb.WriteString(fmt.Sprintf("  %d snapshots", len(snapshots)))
	sb.WriteString("\n\n")
}

func appendHistoryChurnSection(sb *strings.Builder, churn *hpaanalysis.ChurnAnalysis) {
	if churn == nil {
		return
	}
	churnStyle := churnColor(string(churn.Level))
	sb.WriteString(churnStyle.Render(fmt.Sprintf("Churn Score: %d/100 (%s)", churn.Score, churn.Level)))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(fmt.Sprintf(
		"Scale-up: %d | Scale-down: %d | Direction flips: %d",
		churn.ScaleUpCount, churn.ScaleDownCount, churn.DirectionFlips,
	)))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("Time window: %dm", int(churn.TimeWindow.Minutes()))))
	sb.WriteString("\n")
}

func appendHistoryRecommendations(sb *strings.Builder, churn *hpaanalysis.ChurnAnalysis, width int) {
	if churn == nil || len(churn.Recommendations) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Recommendations:"))
	sb.WriteString("\n")
	for _, rec := range churn.Recommendations {
		line := fmt.Sprintf("  - [%s] %s -> %s", rec.Type, rec.CurrentValue, rec.RecommendedValue)
		sb.WriteString(truncate(line, width-2))
		sb.WriteString("\n")
	}
}

func appendHistoryReplicaTrend(sb *strings.Builder, snapshots []hpaanalysis.TimelineSnapshot, churn *hpaanalysis.ChurnAnalysis, graphWidth int) {
	sb.WriteString("\n")
	sb.WriteString("Replica Trend:\n")
	desiredValues := make([]float64, len(snapshots))
	for i, snap := range snapshots {
		desiredValues[i] = float64(snap.Desired)
	}

	sparkStyle := churnSparkStyle(churn)
	flipMarkers := detectDirectionFlips(desiredValues)
	sb.WriteString("  ")
	sb.WriteString(renderSparklineWithMarkers(desiredValues, graphWidth, flipMarkers, sparkStyle))
	sb.WriteString("\n")
	if len(flipMarkers) > 0 {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  %d direction flip(s) detected (↕ = flip point)", len(flipMarkers))))
		sb.WriteString("\n")
	}
}

func churnSparkStyle(churn *hpaanalysis.ChurnAnalysis) lipgloss.Style {
	if churn == nil {
		return okStyle
	}
	switch string(churn.Level) {
	case "MEDIUM":
		return warnStyle
	case "HIGH", "CRITICAL":
		return errorStyle
	}
	return okStyle
}

func appendHistoryMetricTrends(sb *strings.Builder, report *hpaanalysis.StatusReport) {
	if report == nil || len(report.Analysis.Metrics) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString("Metric Trends:\n")
	for _, metric := range report.Analysis.Metrics {
		name := metric.Name
		if name == "" {
			name = metric.Type
		}
		ratioStr := ""
		if metric.Ratio != nil {
			ratioStr = fmt.Sprintf(" %.2f", *metric.Ratio)
		}
		sb.WriteString(fmt.Sprintf("  %-20s%s\n", name, dimStyle.Render(ratioStr)))
	}
}

func appendHistoryHealthTimeline(sb *strings.Builder, snapshots []hpaanalysis.TimelineSnapshot, graphWidth int) {
	sb.WriteString("\n")
	sb.WriteString("Health Timeline:\n")
	sb.WriteString("  ")
	sb.WriteString(renderHealthTimeline(snapshots, graphWidth))
	sb.WriteString("\n")
}

func appendHistoryEventLog(sb *strings.Builder, snapshots []hpaanalysis.TimelineSnapshot, scrollPos, height, width int) {
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Event Log:"))
	sb.WriteString("\n")

	visibleHeight := height - 18 // header + sections + footer
	if visibleHeight < 3 {
		visibleHeight = 3
	}

	start, end := historyScrollWindow(scrollPos, len(snapshots), visibleHeight)

	for i := start; i < end; i++ {
		snap := snapshots[i]
		timeStr := snap.Timestamp.Format("15:04:05")

		replicas := fmt.Sprintf("%d→%d", snap.Current, snap.Desired)
		if snap.Current == snap.Desired {
			replicas = fmt.Sprintf("%d", snap.Desired)
		}

		healthBadge := healthStyle(snap.Health).Render(snap.Health)

		line := fmt.Sprintf("  %s replicas=%s health=%s score=%d",
			timeStr, replicas, healthBadge, snap.HealthScore)
		sb.WriteString(truncate(line, width-2))
		sb.WriteString("\n")
	}

	if len(snapshots) > visibleHeight {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, len(snapshots))))
		sb.WriteString("\n")
	}
}

func historyScrollWindow(scrollPos, totalEntries, visibleHeight int) (int, int) {
	start := scrollPos
	if start < 0 {
		start = 0
	}
	maxStart := totalEntries - visibleHeight
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	end := start + visibleHeight
	if end > totalEntries {
		end = totalEntries
	}
	return start, end
}
