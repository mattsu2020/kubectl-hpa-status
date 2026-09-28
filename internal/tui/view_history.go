package tui

import (
	"fmt"
	"strings"

	hpachurn "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"

	"charm.land/lipgloss/v2"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// historyState holds the history/sparkline view state for a single HPA.
// key anchors the loaded snapshots to one "namespace/name" identity so a
// stale load cannot attach another HPA's history after the cursor moved.
type historyState struct {
	key           string
	loading       bool
	loadErr       error
	snapshots     []hpaanalysis.TimelineSnapshot
	churnAnalysis *hpachurn.ChurnAnalysis
	scrollPos     int
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

// renderHistoryView renders the history/sparkline view for the selected HPA.
func (m Model) renderHistoryView() string {
	items := m.filteredItems()
	if m.cursor >= len(items) {
		return "No HPA selected"
	}

	item := items[m.cursor]

	// Determine available snapshots from history state.
	var snapshots []hpaanalysis.TimelineSnapshot
	var churn *hpachurn.ChurnAnalysis
	var scrollPos int

	if m.historyState != nil {
		hs := m.historyState
		snapshots = hs.snapshots
		churn = hs.churnAnalysis
		scrollPos = hs.scrollPos
	}

	if len(snapshots) == 0 {
		return renderHistoryEmpty(item, m.historyState)
	}

	// Legacy fallback for states whose churn analysis was not computed at
	// load time; updateHistoryLoaded now computes it once per load.
	if churn == nil {
		churn = churnFromSnapshots(snapshots)
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

func renderHistoryEmpty(item hpaanalysis.ListItem, state *historyState) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(fmt.Sprintf("HPA History: %s/%s", item.Namespace, item.Name)))
	sb.WriteString("\n\n")
	switch {
	case state != nil && state.loading:
		sb.WriteString(dimStyle.Render("Loading history from the health store..."))
	case state != nil && state.loadErr != nil:
		sb.WriteString(errorStyle.Render(fmt.Sprintf("History unavailable: %v", state.loadErr)))
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("Snapshots are recorded by status/list --trend; check store permissions under ~/.kube/hpa-status-history."))
	default:
		sb.WriteString(dimStyle.Render("No history recorded for this HPA yet."))
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("Run 'kubectl hpa-status status NAME --trend' (or list --trend) periodically to record snapshots; 'timeline record' captures richer traces."))
	}
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

func appendHistoryChurnSection(sb *strings.Builder, churn *hpachurn.ChurnAnalysis) {
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

func appendHistoryRecommendations(sb *strings.Builder, churn *hpachurn.ChurnAnalysis, width int) {
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

func appendHistoryReplicaTrend(sb *strings.Builder, snapshots []hpaanalysis.TimelineSnapshot, churn *hpachurn.ChurnAnalysis, graphWidth int) {
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

func churnSparkStyle(churn *hpachurn.ChurnAnalysis) lipgloss.Style {
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
	if report == nil || len(report.Analysis.Metrics.Metrics) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString("Metric Trends:\n")
	for _, metric := range report.Analysis.Metrics.Metrics {
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

	start, end := scrollWindow(scrollPos, len(snapshots), visibleHeight)

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
