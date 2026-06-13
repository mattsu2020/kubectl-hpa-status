package tui

import (
	"fmt"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// renderReplayView renders the replay timeline visualization.
func (m Model) renderReplayView() string {
	if m.replayState == nil {
		return dimStyle.Render("No replay data. Press T from detail view to load a trace file.")
	}

	var sb strings.Builder

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

	// Header.
	sb.WriteString(headerStyle.Render(fmt.Sprintf("Replay Timeline: %s/%s", trace.Namespace, trace.HPAName)))
	sb.WriteString(fmt.Sprintf("  %d snapshots  interval=%s", len(trace.Snapshots), trace.Interval))
	sb.WriteString("\n\n")

	// Show replay analysis if available.
	if m.replayState.replayAnalysis != nil {
		analysis := m.replayState.replayAnalysis
		sb.WriteString(headerStyle.Render("Replay Analysis:"))
		sb.WriteString("\n")

		// Show bottleneck count.
		if len(analysis.Bottlenecks) > 0 {
			highCount := 0
			medCount := 0
			for _, b := range analysis.Bottlenecks {
				switch b.Severity {
				case "high":
					highCount++
				case "medium":
					medCount++
				}
			}
			bottleneckText := fmt.Sprintf("  Bottlenecks: %d (", len(analysis.Bottlenecks))
			if highCount > 0 {
				bottleneckText += errorStyle.Render(fmt.Sprintf("%d HIGH", highCount))
			}
			if medCount > 0 {
				if highCount > 0 {
					bottleneckText += ", "
				}
				bottleneckText += warnStyle.Render(fmt.Sprintf("%d MED", medCount))
			}
			bottleneckText += ")"
			sb.WriteString(bottleneckText)
			sb.WriteString("\n")
		}

		// Show control cycle count.
		if len(analysis.ControlCycles) > 0 {
			sb.WriteString(fmt.Sprintf("  Control Cycles: %d\n", len(analysis.ControlCycles)))
		}

		// Show stabilization window count.
		if len(analysis.StabilizationWindows) > 0 {
			sb.WriteString(fmt.Sprintf("  Stabilization Windows: %d\n", len(analysis.StabilizationWindows)))
		}

		sb.WriteString("\n")
	}

	// Timeline entries with visual replica bars.
	const barWidth = 20
	visibleHeight := m.height - 6 // header + footer + padding
	start := m.replayState.scrollPos
	if start < 0 {
		start = 0
	}
	end := start + visibleHeight
	if end > len(trace.Snapshots) {
		end = len(trace.Snapshots)
	}

	maxReplicas := int32(1)
	for _, snap := range trace.Snapshots {
		if snap.Desired > maxReplicas {
			maxReplicas = snap.Desired
		}
	}

	// Show bottleneck markers inline in the timeline.
	bottleneckMarkers := make(map[string]string)
	if m.replayState.replayAnalysis != nil {
		for _, b := range m.replayState.replayAnalysis.Bottlenecks {
			timeKey := b.Timestamp.Format("15:04:05")
			marker := ""
			switch b.Severity {
			case "high":
				marker = errorStyle.Render("[" + b.Type + "]")
			case "medium":
				marker = warnStyle.Render("[" + b.Type + "]")
			default:
				marker = dimStyle.Render("[" + b.Type + "]")
			}
			bottleneckMarkers[timeKey] = marker
		}
	}

	for i := start; i < end; i++ {
		snap := trace.Snapshots[i]
		timeStr := snap.Timestamp.Format("15:04:05")

		// Visual replica bar.
		filled := int(snap.Desired) * barWidth / int(maxReplicas)
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		barStyled := healthStyle(snap.Health).Render(bar)

		// Health badge.
		healthBadge := healthStyle(snap.Health).Render(padRight(snap.Health, 8))

		// Top metric.
		metricInfo := snap.TopMetric
		if metricInfo == "" {
			metricInfo = "-"
		}

		sb.WriteString(fmt.Sprintf("  %s  %s  %s  %s", timeStr, barStyled, healthBadge, metricInfo))

		// Highlight condition changes and events.
		if i > 0 {
			diffs := diffStrings(trace.Snapshots[i-1], snap)
			if len(diffs) > 0 {
				sb.WriteString(fmt.Sprintf("  %s", warnStyle.Render(diffs[0])))
			}
		}

		// Show bottleneck marker if present.
		if marker, ok := bottleneckMarkers[timeStr]; ok {
			sb.WriteString("  " + marker)
		}

		sb.WriteString("\n")
	}

	// Bottlenecks section if replay analysis is available.
	if m.replayState.replayAnalysis != nil && len(m.replayState.replayAnalysis.Bottlenecks) > 0 {
		sb.WriteString("\n")
		sb.WriteString(headerStyle.Render("Bottlenecks:"))
		sb.WriteString("\n")

		for _, b := range m.replayState.replayAnalysis.Bottlenecks {
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

	// Change log section.
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Change Log:"))
	sb.WriteString("\n")

	changeCount := 0
	for i := 1; i < len(trace.Snapshots); i++ {
		diffs := diffStrings(trace.Snapshots[i-1], trace.Snapshots[i])
		if len(diffs) > 0 {
			timeStr := trace.Snapshots[i].Timestamp.Format("15:04:05")
			sb.WriteString(fmt.Sprintf("  %s: %s\n", timeStr, strings.Join(diffs, ", ")))
			changeCount++
			if changeCount >= 10 {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more changes\n", len(trace.Snapshots)-i-1)))
				break
			}
		}
	}

	if changeCount == 0 {
		sb.WriteString(dimStyle.Render("  No changes detected during observation period.\n"))
	}

	// Scroll indicator.
	if len(trace.Snapshots) > visibleHeight {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  [%d-%d of %d] ", start+1, end, len(trace.Snapshots))))
	}

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("j/k=scroll  Esc=back"))

	return sb.String()
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
