package hpa

import (
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// WriteStatusDiff writes a status display that highlights changes between the
// previous and current analysis. Changed fields are shown with emphasis;
// unchanged fields are dimmed when the theme supports it.
//
//nolint:gocyclo // Sequential diff rendering of independent status sections; each section is self-contained.
func WriteStatusDiff(w io.Writer, state WatchState, theme style.Theme) error {
	// When there is no previous state, fall back to full status display.
	if state.Previous == nil {
		return WriteStatusText(w, StatusReport{Analysis: *state.Current}, theme)
	}
	a := state.Current
	prev := state.Previous
	var out []byte

	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "Target: %s\n", a.Target)

	// Replicas: highlight when changed
	desiredChanged := a.Desired != prev.Desired
	currentChanged := a.Current != prev.Current
	desired := theme.ReplicaHighlight(a.Desired, desiredChanged)
	current := fmt.Sprintf("%d", a.Current)
	if currentChanged {
		current = theme.Bold.Render(current)
	}
	out = fmt.Appendf(out, "Replicas: current=%s desired=%s min=%d max=%d\n", current, desired, a.Min, a.Max)

	out = append(out, '\n')
	summaryChanged := a.Summary != prev.Summary
	summaryText := theme.SummaryColor(a.Summary)
	if summaryChanged {
		out = fmt.Appendf(out, "Summary: %s\n", summaryText)
	} else {
		out = fmt.Appendf(out, "Summary: %s\n", theme.Dim.Render(a.Summary))
	}

	// Conditions: highlight changed statuses
	out = append(out, '\n')
	out = append(out, "Conditions:\n"...)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		prevCondMap := conditionMap(prev.Conditions)
		for _, condition := range a.Conditions {
			statusText := theme.ConditionStatus(condition.Type, condition.Status)
			prevStatus := prevCondMap[condition.Type]
			if prevStatus != "" && prevStatus != condition.Status {
				out = fmt.Appendf(out, "  %-15s %s (was %s) %-24s %s\n", condition.Type, statusText, prevStatus, condition.Reason, condition.Message)
			} else {
				out = fmt.Appendf(out, "  %-15s %-7s %-24s %s\n", condition.Type, statusText, condition.Reason, condition.Message)
			}
		}
	}

	// Metrics: highlight changed values
	out = append(out, '\n')
	out = append(out, "Metrics:\n"...)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		prevMetricMap := metricMap(prev.Metrics)
		for _, metric := range a.Metrics {
			note := theme.MetricNote(metric.Note)
			text := formatMetricText(metric, note)
			if prevText, ok := prevMetricMap[metric.Name]; ok && prevText != metric.Text {
				out = fmt.Appendf(out, "  - %s\n", text)
			} else {
				out = fmt.Appendf(out, "  - %s\n", theme.Dim.Render(text))
			}
		}
	}

	if len(a.Behavior) > 0 {
		out = append(out, '\n')
		out = append(out, "Behavior:\n"...)
		for _, behavior := range a.Behavior {
			out = fmt.Appendf(out, "  - %s\n", behavior.Text)
		}
	}

	if len(a.Actions) > 0 {
		out = append(out, '\n')
		out = append(out, "Recommended actions:\n"...)
		for _, action := range a.Actions {
			out = fmt.Appendf(out, "  - %s\n", theme.ActionLine(action))
		}
	}

	if len(a.Interpretation) > 0 {
		out = append(out, '\n')
		out = append(out, "Interpretation:\n"...)
		for _, line := range a.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", theme.InterpretationLine(line))
		}
	}

	_, writeErr := w.Write(out)
	return writeErr
}

// conditionMap builds a map from condition type to status for quick lookup.
func conditionMap(conditions []Condition) map[string]string {
	m := make(map[string]string, len(conditions))
	for _, c := range conditions {
		m[c.Type] = c.Status
	}
	return m
}

// metricMap builds a map from metric name to text for quick lookup.
func metricMap(metrics []Metric) map[string]string {
	m := make(map[string]string, len(metrics))
	for _, metric := range metrics {
		if metric.Name != "" {
			m[metric.Name] = metric.Text
		}
	}
	return m
}
