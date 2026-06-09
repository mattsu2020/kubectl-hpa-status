package hpa

import (
	"fmt"
	"io"
)

// WriteDecisionTraceText writes a standalone decision trace report.
func WriteDecisionTraceText(w io.Writer, trace *DecisionTrace) error {
	var out []byte
	AppendDecisionTraceText(&out, trace)
	_, err := w.Write(out)
	return err
}

// AppendDecisionTraceText appends a human-readable decision trace section.
func AppendDecisionTraceText(out *[]byte, trace *DecisionTrace) {
	if trace == nil {
		return
	}
	*out = append(*out, "Decision trace:\n"...)
	*out = fmt.Appendf(*out, "  1. Current replicas: %d\n", trace.CurrentReplicas)
	step := 2
	if len(trace.Metrics) == 0 {
		*out = fmt.Appendf(*out, "  %d. Metrics: no current metrics are visible in HPA status\n", step)
		step++
	} else {
		for _, metric := range trace.Metrics {
			*out = fmt.Appendf(*out, "  %d. Metric %s:\n", step, metric.Name)
			if metric.Current != "" {
				*out = fmt.Appendf(*out, "       current: %s\n", metric.Current)
			}
			if metric.Target != "" {
				*out = fmt.Appendf(*out, "       target: %s\n", metric.Target)
			}
			if metric.Formula != "" {
				*out = fmt.Appendf(*out, "       raw desired replicas: %s\n", metric.Formula)
			} else {
				*out = fmt.Appendf(*out, "       raw desired replicas: unavailable\n")
			}
			*out = fmt.Appendf(*out, "       confidence: %s\n", metric.Confidence)
			step++
		}
	}
	*out = fmt.Appendf(*out, "  %d. Limit check:\n", step)
	*out = fmt.Appendf(*out, "       maxReplicas: %d\n", trace.MaxReplicas)
	*out = fmt.Appendf(*out, "       result: %s\n", trace.LimitCheck)
	step++
	*out = fmt.Appendf(*out, "  %d. Stabilization:\n", step)
	*out = fmt.Appendf(*out, "       %s\n", trace.Stabilization)
	step++
	*out = fmt.Appendf(*out, "  %d. Final interpretation:\n", step)
	*out = fmt.Appendf(*out, "       %s\n", trace.FinalInterpretation)
	*out = fmt.Appendf(*out, "       confidence: %s\n", trace.Confidence)
	if len(trace.Notes) > 0 {
		*out = append(*out, "       notes:\n"...)
		for _, note := range trace.Notes {
			*out = fmt.Appendf(*out, "         - %s\n", note)
		}
	}
}
