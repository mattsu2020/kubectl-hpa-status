package tui

import (
	"fmt"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/retrospective"
)

// renderReplayView renders the replay timeline visualization.
func (m Model) renderReplayView() string {
	if m.replayState == nil {
		return dimStyle.Render("No replay data. Press T from detail view to load a trace file.")
	}

	if m.replayState.loading {
		return "Loading replay trace..."
	}

	if m.replayState.err != nil {
		return errorStyle.Render(fmt.Sprintf("Replay error: %v", m.replayState.err))
	}

	trace := m.replayState.trace
	if trace == nil || len(trace.Snapshots) == 0 {
		return dimStyle.Render("No snapshots in trace.")
	}

	var sb strings.Builder
	appendReplayHeader(&sb, trace)
	appendReplayAnalysisSummary(&sb, m.replayState.replayAnalysis)

	visibleHeight := m.height - 6 // header + footer + padding
	start, end := replayVisibleRange(trace, m.replayState.scrollPos, visibleHeight)
	maxReplicas := replayMaxReplicas(trace.Snapshots)
	bottleneckMarkers := replayBottleneckMarkers(m.replayState.replayAnalysis)

	appendReplayTimelineRows(&sb, trace, start, end, maxReplicas, bottleneckMarkers)
	appendReplayBottlenecksSection(&sb, m.replayState.replayAnalysis)
	appendReplayChangeLog(&sb, trace)

	if len(trace.Snapshots) > visibleHeight {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  [%d-%d of %d] ", start+1, end, len(trace.Snapshots))))
	}

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("j/k=scroll  Esc=back"))

	return sb.String()
}

func appendReplayHeader(sb *strings.Builder, trace *hpaanalysis.TimelineTrace) {
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Replay Timeline: %s/%s", trace.Namespace, trace.HPAName)))
	sb.WriteString(fmt.Sprintf("  %d snapshots  interval=%s", len(trace.Snapshots), trace.Interval))
	sb.WriteString("\n\n")
}

func appendReplayAnalysisSummary(sb *strings.Builder, analysis *retrospective.ReplayAnalysis) {
	if analysis == nil {
		return
	}
	sb.WriteString(headerStyle.Render("Replay Analysis:"))
	sb.WriteString("\n")

	if len(analysis.Bottlenecks) > 0 {
		highCount, medCount := countBottlenecksBySeverity(analysis.Bottlenecks)
		sb.WriteString(formatBottleneckSummary(highCount, medCount, len(analysis.Bottlenecks)))
		sb.WriteString("\n")
	}
	if len(analysis.ControlCycles) > 0 {
		sb.WriteString(fmt.Sprintf("  Control Cycles: %d\n", len(analysis.ControlCycles)))
	}
	if len(analysis.StabilizationWindows) > 0 {
		sb.WriteString(fmt.Sprintf("  Stabilization Windows: %d\n", len(analysis.StabilizationWindows)))
	}
	sb.WriteString("\n")
}

func countBottlenecksBySeverity(bottlenecks []retrospective.BottleneckMarker) (highCount, medCount int) {
	for _, b := range bottlenecks {
		switch b.Severity {
		case "high":
			highCount++
		case "medium":
			medCount++
		}
	}
	return highCount, medCount
}

func formatBottleneckSummary(highCount, medCount, total int) string {
	text := fmt.Sprintf("  Bottlenecks: %d (", total)
	if highCount > 0 {
		text += errorStyle.Render(fmt.Sprintf("%d HIGH", highCount))
	}
	if medCount > 0 {
		if highCount > 0 {
			text += ", "
		}
		text += warnStyle.Render(fmt.Sprintf("%d MED", medCount))
	}
	text += ")"
	return text
}

func replayVisibleRange(trace *hpaanalysis.TimelineTrace, scrollPos, visibleHeight int) (int, int) {
	start := scrollPos
	if start < 0 {
		start = 0
	}
	end := start + visibleHeight
	if end > len(trace.Snapshots) {
		end = len(trace.Snapshots)
	}
	return start, end
}

func replayMaxReplicas(snapshots []hpaanalysis.TimelineSnapshot) int32 {
	maxReplicas := int32(1)
	for _, snap := range snapshots {
		if snap.Desired > maxReplicas {
			maxReplicas = snap.Desired
		}
	}
	return maxReplicas
}

func replayBottleneckMarkers(analysis *retrospective.ReplayAnalysis) map[string]string {
	markers := make(map[string]string)
	if analysis == nil {
		return markers
	}
	for _, b := range analysis.Bottlenecks {
		timeKey := b.Timestamp.Format("15:04:05")
		var marker string
		switch b.Severity {
		case "high":
			marker = errorStyle.Render("[" + b.Type + "]")
		case "medium":
			marker = warnStyle.Render("[" + b.Type + "]")
		default:
			marker = dimStyle.Render("[" + b.Type + "]")
		}
		markers[timeKey] = marker
	}
	return markers
}

func appendReplayTimelineRows(sb *strings.Builder, trace *hpaanalysis.TimelineTrace, start, end int, maxReplicas int32, bottleneckMarkers map[string]string) {
	const barWidth = 20
	for i := start; i < end; i++ {
		snap := trace.Snapshots[i]
		timeStr := snap.Timestamp.Format("15:04:05")

		filled := int(snap.Desired) * barWidth / int(maxReplicas)
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		barStyled := healthStyle(snap.Health).Render(bar)
		healthBadge := healthStyle(snap.Health).Render(padRight(snap.Health, 8))

		metricInfo := snap.TopMetric
		if metricInfo == "" {
			metricInfo = "-"
		}

		sb.WriteString(fmt.Sprintf("  %s  %s  %s  %s", timeStr, barStyled, healthBadge, metricInfo))

		if i > 0 {
			diffs := diffStrings(trace.Snapshots[i-1], snap)
			if len(diffs) > 0 {
				sb.WriteString(fmt.Sprintf("  %s", warnStyle.Render(diffs[0])))
			}
		}

		if marker, ok := bottleneckMarkers[timeStr]; ok {
			sb.WriteString("  " + marker)
		}

		sb.WriteString("\n")
	}
}

func appendReplayBottlenecksSection(sb *strings.Builder, analysis *retrospective.ReplayAnalysis) {
	if analysis == nil || len(analysis.Bottlenecks) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Bottlenecks:"))
	sb.WriteString("\n")

	for _, b := range analysis.Bottlenecks {
		timeStr := b.Timestamp.Format("15:04:05")
		var severityMarker string
		switch b.Severity {
		case "high":
			severityMarker = errorStyle.Render("[" + b.Severity + "]")
		case "medium":
			severityMarker = warnStyle.Render("[" + b.Severity + "]")
		default:
			severityMarker = dimStyle.Render("[" + b.Severity + "]")
		}
		sb.WriteString(fmt.Sprintf("  %s  %s %s — %s\n",
			timeStr, severityMarker, b.Type, b.Message))
	}
	sb.WriteString("\n")
}

func appendReplayChangeLog(sb *strings.Builder, trace *hpaanalysis.TimelineTrace) {
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Change Log:"))
	sb.WriteString("\n")

	changeCount := 0
	for i := 1; i < len(trace.Snapshots); i++ {
		diffs := diffStrings(trace.Snapshots[i-1], trace.Snapshots[i])
		if len(diffs) == 0 {
			continue
		}
		timeStr := trace.Snapshots[i].Timestamp.Format("15:04:05")
		sb.WriteString(fmt.Sprintf("  %s: %s\n", timeStr, strings.Join(diffs, ", ")))
		changeCount++
		if changeCount >= 10 {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more changes\n", len(trace.Snapshots)-i-1)))
			break
		}
	}

	if changeCount == 0 {
		sb.WriteString(dimStyle.Render("  No changes detected during observation period.\n"))
	}
}

// diffStrings compares two timeline snapshots and returns human-readable diff lines.
func diffStrings(prev, curr hpaanalysis.TimelineSnapshot) []string {
	var diffs []string

	if prev.Desired != curr.Desired {
		diffs = append(diffs, fmt.Sprintf("replicas %d→%d", prev.Desired, curr.Desired))
	}

	if prev.Health != curr.Health {
		diffs = append(diffs, fmt.Sprintf("health %s→%s", prev.Health, curr.Health))
	}

	if prev.HealthScore != curr.HealthScore {
		diffs = append(diffs, fmt.Sprintf("score %d→%d", prev.HealthScore, curr.HealthScore))
	}

	// Condition changes.
	prevConds := conditionMap(prev.Conditions)
	currConds := conditionMap(curr.Conditions)
	for k, cv := range currConds {
		pv, existed := prevConds[k]
		if !existed || pv != cv {
			diffs = append(diffs, fmt.Sprintf("%s: %s", k, cv))
		}
	}

	return diffs
}

func conditionMap(conditions []hpaanalysis.Condition) map[string]string {
	m := make(map[string]string, len(conditions))
	for _, c := range conditions {
		m[c.Type] = c.Status
	}
	return m
}
