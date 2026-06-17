package hpa

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// This file holds the smaller section renderers extracted from
// WriteStatusDiff (diff_text.go) so the orchestrator stays a flat list of
// section calls without a gocyclo exemption.

func appendDiffConditionsSection(out *[]byte, a, prev *Analysis, theme style.Theme) {
	*out = append(*out, '\n')
	*out = append(*out, "Conditions:\n"...)
	if len(a.Conditions) == 0 {
		*out = append(*out, "  No conditions reported.\n"...)
		return
	}
	prevCondMap := conditionMap(prev.Conditions)
	for _, condition := range a.Conditions {
		statusText := theme.ConditionStatus(condition.Type, condition.Status)
		prevStatus := prevCondMap[condition.Type]
		if prevStatus != "" && prevStatus != condition.Status {
			*out = fmt.Appendf(*out, "  %-15s %s (was %s) %-24s %s\n", condition.Type, statusText, prevStatus, condition.Reason, condition.Message)
		} else {
			*out = fmt.Appendf(*out, "  %-15s %-7s %-24s %s\n", condition.Type, statusText, condition.Reason, condition.Message)
		}
	}
}

func appendDiffMetricsSection(out *[]byte, a, prev *Analysis, theme style.Theme) {
	*out = append(*out, '\n')
	*out = append(*out, "Metrics:\n"...)
	if len(a.Metrics) == 0 {
		*out = append(*out, "  No current metrics reported.\n"...)
		return
	}
	prevMetricMap := metricMap(prev.Metrics)
	for _, metric := range a.Metrics {
		note := theme.MetricNote(metric.Note)
		text := formatMetricText(metric, note)
		if prevText, ok := prevMetricMap[metric.Name]; ok && prevText != metric.Text {
			*out = fmt.Appendf(*out, "  - %s\n", text)
		} else {
			*out = fmt.Appendf(*out, "  - %s\n", theme.Dim.Render(text))
		}
	}
}

func appendDiffBehaviorSection(out *[]byte, a *Analysis) {
	if len(a.Behavior) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Behavior:\n"...)
	for _, behavior := range a.Behavior {
		*out = fmt.Appendf(*out, "  - %s\n", behavior.Text)
	}
}

func appendDiffActionsSection(out *[]byte, a *Analysis, theme style.Theme) {
	if len(a.Actions) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Recommended actions:\n"...)
	for _, action := range a.Actions {
		*out = fmt.Appendf(*out, "  - %s\n", theme.ActionLine(action))
	}
}

func appendDiffInterpretationSection(out *[]byte, a *Analysis, theme style.Theme) {
	if len(a.Interpretation) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Interpretation:\n"...)
	for _, line := range a.Interpretation {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(line))
	}
}
