package hpa

import (
	"fmt"
	"io"
)

type StatusReport struct {
	Analysis Analysis `json:"analysis" yaml:"analysis"`
	Events   []Event  `json:"events,omitempty" yaml:"events,omitempty"`
}

func WriteStatusText(w io.Writer, report StatusReport) error {
	a := report.Analysis
	var out []byte
	out = fmt.Appendf(out, "HPA %s/%s\n", a.Namespace, a.Name)
	out = fmt.Appendf(out, "Target: %s\n", a.Target)
	out = fmt.Appendf(out, "Replicas: current=%d desired=%d min=%d max=%d\n", a.Current, a.Desired, a.Min, a.Max)

	out = append(out, '\n')
	out = fmt.Appendf(out, "Summary: %s\n", a.Summary)

	out = append(out, '\n')
	out = append(out, "Conditions:\n"...)
	if len(a.Conditions) == 0 {
		out = append(out, "  No conditions reported.\n"...)
	} else {
		for _, condition := range a.Conditions {
			out = fmt.Appendf(out, "  %-15s %-7s %-24s %s\n", condition.Type, condition.Status, condition.Reason, condition.Message)
		}
	}

	out = append(out, '\n')
	out = append(out, "Metrics:\n"...)
	if len(a.Metrics) == 0 {
		out = append(out, "  No current metrics reported.\n"...)
	} else {
		for _, metric := range a.Metrics {
			out = fmt.Appendf(out, "  - %s\n", metric.Text)
		}
	}

	if len(a.Interpretation) > 0 {
		out = append(out, '\n')
		out = append(out, "Interpretation:\n"...)
		for _, line := range a.Interpretation {
			out = fmt.Appendf(out, "  - %s\n", line)
		}
	}

	out = append(out, '\n')
	out = append(out, "Recent events:\n"...)
	if len(report.Events) == 0 {
		out = append(out, "  No recent events found.\n"...)
	} else {
		for _, event := range report.Events {
			out = fmt.Appendf(out, "  - %s: %s\n", event.Reason, event.Message)
		}
	}

	_, err := w.Write(out)
	return err
}

type ListItem struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Name      string `json:"name" yaml:"name"`
	Target    string `json:"target" yaml:"target"`
	Current   int32  `json:"currentReplicas" yaml:"currentReplicas"`
	Desired   int32  `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min       int32  `json:"minReplicas" yaml:"minReplicas"`
	Max       int32  `json:"maxReplicas" yaml:"maxReplicas"`
	Summary   string `json:"summary" yaml:"summary"`
	Health    string `json:"health" yaml:"health"`
	Issue     string `json:"issue,omitempty" yaml:"issue,omitempty"`
}

type ListReport struct {
	Items []ListItem `json:"items" yaml:"items"`
}

func NewListItem(src Analysis) ListItem {
	issue := ""
	health := "OK"
	for _, condition := range src.Conditions {
		if condition.Type == "ScalingActive" && condition.Status != "True" {
			health = "ERROR"
			issue = "ERROR: " + condition.Reason
			break
		}
		if condition.Type == "ScalingLimited" && condition.Status == "True" {
			health = "LIMITED"
			issue = "LIMITED: " + condition.Reason
		}
	}
	return ListItem{
		Namespace: src.Namespace,
		Name:      src.Name,
		Target:    src.Target,
		Current:   src.Current,
		Desired:   src.Desired,
		Min:       src.Min,
		Max:       src.Max,
		Summary:   src.Summary,
		Health:    health,
		Issue:     issue,
	}
}

func WriteListText(w io.Writer, report ListReport, wide bool) error {
	var out []byte
	if wide {
		out = fmt.Appendf(out, "%-20s %-32s %-28s %-8s %-8s %-8s %-8s %-10s %-32s %s\n", "NAMESPACE", "NAME", "TARGET", "CURRENT", "DESIRED", "MIN", "MAX", "HEALTH", "ISSUE", "SUMMARY")
		for _, item := range report.Items {
			out = fmt.Appendf(out, "%-20s %-32s %-28s %-8d %-8d %-8d %-8d %-10s %-32s %s\n", item.Namespace, item.Name, item.Target, item.Current, item.Desired, item.Min, item.Max, item.Health, item.Issue, item.Summary)
		}
		_, err := w.Write(out)
		return err
	}

	out = fmt.Appendf(out, "%-20s %-32s %-8s %-8s %-10s %-32s %s\n", "NAMESPACE", "NAME", "CURRENT", "DESIRED", "HEALTH", "ISSUE", "SUMMARY")
	for _, item := range report.Items {
		out = fmt.Appendf(out, "%-20s %-32s %-8d %-8d %-10s %-32s %s\n", item.Namespace, item.Name, item.Current, item.Desired, item.Health, item.Issue, item.Summary)
	}
	_, err := w.Write(out)
	return err
}
