package hpa

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// This file holds the smaller section renderers extracted from
// WriteStatusDiff (diff_text.go) so the orchestrator stays a flat list of
// section calls without a gocyclo exemption.

func appendDiffConditionsSection(out *[]byte, a, prev *Analysis, theme style.Theme, labels labels) {
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Conditions)
	if len(a.Conditions) == 0 {
		*out = append(*out, "  No conditions reported.\n"...)
		return
	}
	prevCondMap := conditionMap(prev.Conditions)
	for _, condition := range a.Conditions {
		statusText := theme.ConditionStatus(condition.Type, condition.Status)
		previous, found := prevCondMap[condition.Type]
		switch {
		case found && previous.Status != condition.Status:
			*out = fmt.Appendf(*out, "  %-15s %s (was %s) %-24s %s\n", SanitizeTerminalText(string(condition.Type)), statusText, previous.Status, SanitizeTerminalText(condition.Reason), SanitizeTerminalText(condition.Message))
		case !found || previous.Reason != condition.Reason || previous.Message != condition.Message:
			*out = fmt.Appendf(*out, "  %-15s %-7s %-24s %s (changed)\n", SanitizeTerminalText(string(condition.Type)), statusText, SanitizeTerminalText(condition.Reason), SanitizeTerminalText(condition.Message))
		default:
			*out = fmt.Appendf(*out, "  %-15s %-7s %-24s %s\n", SanitizeTerminalText(string(condition.Type)), statusText, SanitizeTerminalText(condition.Reason), SanitizeTerminalText(condition.Message))
		}
	}
}

func appendDiffMetricsSection(out *[]byte, a, prev *Analysis, theme style.Theme, labels labels) {
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Metrics)
	if len(a.Metrics) == 0 {
		*out = append(*out, "  No current metrics reported.\n"...)
		return
	}
	prevMetricMap := metricMap(prev.Metrics)
	for _, metric := range a.Metrics {
		note := theme.MetricNote(metric.Note)
		text := formatMetricText(metric, note)
		if previous, ok := prevMetricMap[metricIdentity(metric)]; !ok || !metricEqual(previous, metric) {
			*out = fmt.Appendf(*out, "  - %s\n", text)
		} else {
			*out = fmt.Appendf(*out, "  - %s\n", theme.Dim.Render(text))
		}
	}
}

func appendDiffBehaviorSection(out *[]byte, a *Analysis, labels labels) {
	if len(a.Behavior) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Behavior)
	for _, behavior := range a.Behavior {
		*out = fmt.Appendf(*out, "  - %s\n", behavior.Text)
	}
}

func appendDiffActionsSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Actions) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Actions)
	for _, action := range a.Actions {
		*out = fmt.Appendf(*out, "  - %s\n", theme.ActionLine(action))
	}
}

func appendDiffInterpretationSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Interpretation) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Interpretation)
	for _, line := range a.Interpretation {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(line))
	}
}
