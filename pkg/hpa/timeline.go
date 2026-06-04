package hpa

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

// SnapshotFromReport converts a StatusReport into a compact TimelineSnapshot
// suitable for time-series recording.
func SnapshotFromReport(report StatusReport) TimelineSnapshot {
	a := report.Analysis
	return TimelineSnapshot{
		Timestamp:      time.Now(),
		Current:        a.Current,
		Desired:        a.Desired,
		Health:         a.Health,
		HealthScore:    a.HealthScore,
		TopMetric:      topMetricFromAnalysis(&a),
		Conditions:     a.Conditions,
		Summary:        a.Summary,
		Interpretation: a.Interpretation,
		Events:         report.Events,
	}
}

// topMetricFromAnalysis extracts the most influential metric description.
func topMetricFromAnalysis(a *Analysis) string {
	if a.ImpactMetric != nil {
		return fmt.Sprintf("%s (ratio=%.2f %s)", a.ImpactMetric.Name, a.ImpactMetric.Ratio, a.ImpactMetric.Note)
	}
	if len(a.Metrics) > 0 {
		return a.Metrics[0].Text
	}
	return ""
}

// WriteTimelineTable renders a TimelineTrace as a fixed-width terminal table.
func WriteTimelineTable(w io.Writer, trace TimelineTrace, theme style.Theme) error {
	var out strings.Builder

	out.WriteString(fmt.Sprintf("HPA Timeline: %s/%s  interval=%s  snapshots=%d\n\n",
		trace.Namespace, trace.HPAName, trace.Interval, len(trace.Snapshots)))

	// Header
	out.WriteString(fmt.Sprintf("%-10s %-14s %-14s %-30s %s\n",
		"TIME", "REPLICAS", "HEALTH", "TOP SIGNAL", "INTERPRETATION"))
	out.WriteString(strings.Repeat("-", 100) + "\n")

	for i, snap := range trace.Snapshots {
		timeStr := snap.Timestamp.Format("15:04:05")

		replicas := fmt.Sprintf("%d -> %d", snap.Current, snap.Desired)
		if snap.Current == snap.Desired {
			replicas = fmt.Sprintf("%d", snap.Current)
		}

		health := theme.HealthLabel(snap.Health, snap.HealthScore)

		topSignal := snap.TopMetric
		if len(topSignal) > 30 {
			topSignal = topSignal[:27] + "..."
		}

		interpretation := ""
		if len(snap.Interpretation) > 0 {
			interpretation = snap.Interpretation[0]
			if len(interpretation) > 50 {
				interpretation = interpretation[:47] + "..."
			}
		}

		out.WriteString(fmt.Sprintf("%-10s %-14s %-14s %-30s %s\n",
			timeStr, replicas, health, topSignal, interpretation))

		// Show event changes
		for _, event := range snap.Events {
			msg := event.Message
			if len(msg) > 80 {
				msg = msg[:77] + "..."
			}
			out.WriteString(fmt.Sprintf("  event: %s: %s\n", event.Reason, msg))
		}

		// Show condition changes between snapshots
		if i > 0 {
			prev := trace.Snapshots[i-1]
			changes := DiffSnapshots(prev, snap)
			for _, change := range changes {
				out.WriteString(fmt.Sprintf("  change: %s\n", change))
			}
		}
	}

	_, err := io.WriteString(w, out.String())
	return err
}

// WriteTimelineMarkdown renders a TimelineTrace as a Markdown table.
func WriteTimelineMarkdown(w io.Writer, trace TimelineTrace) error {
	var out strings.Builder

	out.WriteString(fmt.Sprintf("# HPA Timeline: %s/%s\n\n", trace.Namespace, trace.HPAName))
	out.WriteString(fmt.Sprintf("- **Interval:** %s\n- **Snapshots:** %d\n- **Start:** %s\n",
		trace.Interval, len(trace.Snapshots), trace.Start.Format(time.RFC3339)))
	if !trace.End.IsZero() {
		out.WriteString(fmt.Sprintf("- **End:** %s\n", trace.End.Format(time.RFC3339)))
	}
	out.WriteString("\n")

	out.WriteString("| Time | Current | Desired | Health | Score | Top Metric | Summary |\n")
	out.WriteString("|------|---------|---------|--------|-------|------------|--------|\n")

	for _, snap := range trace.Snapshots {
		topMetric := snap.TopMetric
		if topMetric == "" {
			topMetric = "-"
		}
		summary := snap.Summary
		if summary == "" {
			summary = "-"
		}
		out.WriteString(fmt.Sprintf("| %s | %d | %d | %s | %d | %s | %s |\n",
			snap.Timestamp.Format("15:04:05"),
			snap.Current,
			snap.Desired,
			snap.Health,
			snap.HealthScore,
			escapeMarkdown(topMetric),
			escapeMarkdown(summary)))
	}
	out.WriteString("\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// WriteTimelineHTML renders a TimelineTrace as a standalone HTML document.
func WriteTimelineHTML(w io.Writer, trace TimelineTrace) error {
	var out strings.Builder

	out.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>HPA Timeline: `)
	out.WriteString(htmlEscape(trace.HPAName))
	out.WriteString("</title>\n<style>\n")
	out.WriteString(htmlCSS())
	out.WriteString("</style>\n</head>\n<body>\n")

	out.WriteString(fmt.Sprintf("<h1>HPA Timeline: %s/%s</h1>\n", htmlEscape(trace.Namespace), htmlEscape(trace.HPAName)))
	out.WriteString(fmt.Sprintf("<p>Interval: %s | Snapshots: %d | Start: %s",
		trace.Interval, len(trace.Snapshots), trace.Start.Format(time.RFC3339)))
	if !trace.End.IsZero() {
		out.WriteString(fmt.Sprintf(" | End: %s", trace.End.Format(time.RFC3339)))
	}
	out.WriteString("</p>\n")

	if len(trace.Snapshots) > 0 {
		out.WriteString("<table>\n<tr><th>Time</th><th>Current</th><th>Desired</th><th>Health</th><th>Score</th><th>Top Metric</th><th>Summary</th></tr>\n")
		for _, snap := range trace.Snapshots {
			out.WriteString("<tr>")
			out.WriteString(fmt.Sprintf("<td>%s</td>", snap.Timestamp.Format("15:04:05")))
			out.WriteString(fmt.Sprintf("<td>%d</td>", snap.Current))
			out.WriteString(fmt.Sprintf("<td>%d</td>", snap.Desired))
			out.WriteString(fmt.Sprintf("<td>%s</td>", htmlHealthBadge(snap.Health, snap.HealthScore)))
			out.WriteString(fmt.Sprintf("<td>%d</td>", snap.HealthScore))
			out.WriteString(fmt.Sprintf("<td>%s</td>", htmlEscape(snap.TopMetric)))
			out.WriteString(fmt.Sprintf("<td>%s</td>", htmlEscape(snap.Summary)))
			out.WriteString("</tr>\n")
		}
		out.WriteString("</table>\n")
	}

	out.WriteString("<footer>Generated by kubectl-hpa-status</footer>\n")
	out.WriteString("</body>\n</html>\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// DiffSnapshots compares two consecutive snapshots and returns human-readable
// change descriptions.
func DiffSnapshots(prev, curr TimelineSnapshot) []string {
	var changes []string

	if prev.Current != curr.Current || prev.Desired != curr.Desired {
		changes = append(changes, fmt.Sprintf("replicas: %d/%d -> %d/%d",
			prev.Current, prev.Desired, curr.Current, curr.Desired))
	}

	if prev.Health != curr.Health {
		changes = append(changes, fmt.Sprintf("health: %s -> %s", prev.Health, curr.Health))
	}

	if prev.HealthScore != curr.HealthScore {
		changes = append(changes, fmt.Sprintf("healthScore: %d -> %d", prev.HealthScore, curr.HealthScore))
	}

	prevConditions := timelineConditionMap(prev.Conditions)
	currConditions := timelineConditionMap(curr.Conditions)
	for t, c := range currConditions {
		if p, ok := prevConditions[t]; !ok || p.Status != c.Status || p.Reason != c.Reason {
			prevStatus := p.Status
			prevReason := p.Reason
			changes = append(changes, fmt.Sprintf("condition %s: %s/%s -> %s/%s",
				t, prevStatus, prevReason, c.Status, c.Reason))
		}
	}

	return changes
}

// timelineConditionMap builds a map of condition type to condition for quick lookup.
func timelineConditionMap(conditions []Condition) map[string]Condition {
	m := make(map[string]Condition, len(conditions))
	for _, c := range conditions {
		m[c.Type] = c
	}
	return m
}
