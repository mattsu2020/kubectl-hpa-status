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
	labels := resolveLabels(opts.Labels)
	var out []byte

	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "%s: %s\n", labels.Target, a.Target)

	// Replicas: highlight when changed
	desiredChanged := a.Desired != prev.Desired
	currentChanged := a.Current != prev.Current
	desired := theme.ReplicaHighlight(a.Desired, desiredChanged)
	current := fmt.Sprintf("%d", a.Current)
	if currentChanged {
		current = theme.Bold.Render(current)
	}
	out = fmt.Appendf(out, "%s: current=%s desired=%s min=%d max=%d\n", labels.Replicas, current, desired, a.Min, a.Max)

	out = append(out, '\n')
	summary := opts.translateSummary(a.Summary, a.SummaryKey)
	summaryChanged := a.Summary != prev.Summary
	if summaryChanged {
		out = fmt.Appendf(out, "%s: %s\n", labels.Summary, theme.SummaryColorForKey(summary, a.SummaryKey))
	} else {
		out = fmt.Appendf(out, "%s: %s\n", labels.Summary, theme.Dim.Render(summary))
	}

	appendDiffConditionsSection(&out, a, prev, theme, labels)
	appendDiffMetricsSection(&out, a, prev, theme, labels)
	appendDiffBehaviorSection(&out, a, labels)
	appendDiffActionsSection(&out, a, theme, labels)
	appendDiffInterpretationSection(&out, a, theme, labels)

	_, writeErr := w.Write(out)
	return writeErr
}

// conditionMap builds a map from condition type to the full observable value.
func conditionMap(conditions []Condition) map[string]Condition {
	m := make(map[string]Condition, len(conditions))
	for _, c := range conditions {
		m[c.Type] = c
	}
	return m
}

// metricMap uses all identity fields so object/external metrics with the same
// name or an empty name cannot collide.
func metricMap(metrics []Metric) map[string]Metric {
	m := make(map[string]Metric, len(metrics))
	for _, metric := range metrics {
		m[metricIdentity(metric)] = metric
	}
	return m
}

func metricIdentity(metric Metric) string {
	return metric.Type + "\x00" + metric.Name + "\x00" + metric.Selector + "\x00" + metric.Object
}

func metricEqual(a, b Metric) bool {
	if a.Type != b.Type || a.Name != b.Name || a.Selector != b.Selector || a.Object != b.Object ||
		a.Current != b.Current || a.Target != b.Target || a.Note != b.Note {
		return false
	}
	if a.Ratio == nil || b.Ratio == nil {
		return a.Ratio == nil && b.Ratio == nil
	}
	return *a.Ratio == *b.Ratio
}
