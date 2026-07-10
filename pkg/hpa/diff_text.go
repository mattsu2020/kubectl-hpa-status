package hpa

import (
	"fmt"
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// WriteStatusDiff writes a status display that highlights changes between the
// previous and current analysis. Changed fields are shown with emphasis;
// unchanged fields are dimmed when the theme supports it.
func WriteStatusDiff(w io.Writer, state WatchState, theme style.Theme) error {
	return WriteStatusDiffWithOptions(w, state, StatusTextOptions{Theme: theme})
}

// WriteStatusDiffWithOptions writes the change-highlighting status display
// with full rendering options, localising the Summary line via
// opts.SummaryTranslator when configured.
func WriteStatusDiffWithOptions(w io.Writer, state WatchState, opts StatusTextOptions) error {
	theme := opts.Theme
	// When there is no previous state, fall back to full status display.
	if state.Previous == nil {
		return WriteStatusTextWithOptions(w, StatusReport{Analysis: *state.Current}, opts)
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
	summary := opts.translateSummary(a.Summary, a.SummaryKey)
	summaryChanged := a.Summary != prev.Summary
	if summaryChanged {
		out = fmt.Appendf(out, "Summary: %s\n", theme.SummaryColor(summary))
	} else {
		out = fmt.Appendf(out, "Summary: %s\n", theme.Dim.Render(summary))
	}

	appendDiffConditionsSection(&out, a, prev, theme)
	appendDiffMetricsSection(&out, a, prev, theme)
	appendDiffBehaviorSection(&out, a)
	appendDiffActionsSection(&out, a, theme)
	appendDiffInterpretationSection(&out, a, theme)

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
