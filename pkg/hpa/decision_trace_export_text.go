package hpa

import "fmt"

// AppendStructuredDecisionTraceText appends a human-readable structured
// decision trace section to the output buffer.
func AppendStructuredDecisionTraceText(out *[]byte, trace *StructuredDecisionTrace, labels LabelProvider) {
	if trace == nil {
		return
	}

	label := "Structured Decision Trace"
	if labels != nil {
		if v := labels.Get("label_structured_decision_trace"); v != "label_structured_decision_trace" {
			label = v
		}
	}

	appendDecisionTraceHeader(out, label, trace)
	appendDecisionTraceMetrics(out, trace)

	// Winner metric.
	if trace.WinnerMetric != "" {
		*out = fmt.Appendf(*out, "\n  Winner metric: %s (confidence: %s)\n",
			trace.WinnerMetric, string(trace.WinnerConfidence))
	}

	// Limit clamp.
	if trace.LimitClamp != "" {
		*out = fmt.Appendf(*out, "\n  Limit clamp: %s\n", trace.LimitClamp)
	}

	appendDecisionTraceTolerance(out, trace)
	appendDecisionTraceStabilization(out, trace)
	appendDecisionTraceDecisionPath(out, trace)

	// Summary.
	if trace.Summary != "" {
		*out = fmt.Appendf(*out, "\n  Summary: %s\n", trace.Summary)
	}
}

// appendDecisionTraceHeader writes the label, schema, HPA, replica and
// estimated raw desired lines for the structured decision trace.
func appendDecisionTraceHeader(out *[]byte, label string, trace *StructuredDecisionTrace) {
	*out = fmt.Appendf(*out, "%s:\n", label)
	*out = fmt.Appendf(*out, "  schema: %s\n", trace.SchemaVersion)
	*out = fmt.Appendf(*out, "  HPA: %s/%s\n", trace.Namespace, trace.Name)
	*out = fmt.Appendf(*out, "  replicas: current=%d desired=%d min=%d max=%d\n",
		trace.CurrentReplicas, trace.VisibleDesiredReplicas, trace.MinReplicas, trace.MaxReplicas)

	if trace.EstimatedRawDesired > 0 {
		*out = fmt.Appendf(*out, "  estimated raw desired: %d\n", trace.EstimatedRawDesired)
	}
}

// appendDecisionTraceMetrics renders the per-metric analysis table.
func appendDecisionTraceMetrics(out *[]byte, trace *StructuredDecisionTrace) {
	if len(trace.Metrics) == 0 {
		return
	}

	*out = append(*out, "\n  Metrics analysis:\n"...)
	for _, m := range trace.Metrics {
		appendDecisionTraceMetricRow(out, m)
	}
}

// appendDecisionTraceMetricRow renders a single metric analysis row.
func appendDecisionTraceMetricRow(out *[]byte, m StructuredMetricTrace) {
	*out = fmt.Appendf(*out, "    %-20s type=%-10s current=%s target=%s",
		m.Name, m.Type, m.Current, m.Target)
	if m.Ratio != nil {
		*out = fmt.Appendf(*out, " ratio=%.3f", *m.Ratio)
	}
	if m.DistanceFromTarget > 0 {
		*out = fmt.Appendf(*out, " distance=%.3f", m.DistanceFromTarget)
	}
	*out = fmt.Appendf(*out, " direction=%s", m.DesiredDirection)
	if m.WithinTolerance {
		*out = append(*out, " [within tolerance]"...)
	}
	*out = append(*out, '\n')
	if m.Formula != "" {
		*out = fmt.Appendf(*out, "      formula: %s\n", m.Formula)
	}
}

// appendDecisionTraceTolerance renders the tolerance effect block.
func appendDecisionTraceTolerance(out *[]byte, trace *StructuredDecisionTrace) {
	if trace.ToleranceEffect == nil {
		return
	}

	*out = append(*out, "\n  Tolerance effect:\n"...)
	*out = fmt.Appendf(*out, "    effective tolerance: %.2f\n", trace.ToleranceEffect.EffectiveTolerance)
	if len(trace.ToleranceEffect.SuppressedMetrics) > 0 {
		*out = fmt.Appendf(*out, "    suppressed metrics: %s\n",
			joinStrings(trace.ToleranceEffect.SuppressedMetrics, ", "))
	}
	if trace.ToleranceEffect.Note != "" {
		*out = fmt.Appendf(*out, "    %s\n", trace.ToleranceEffect.Note)
	}
}

// appendDecisionTraceStabilization renders the stabilization effect block.
func appendDecisionTraceStabilization(out *[]byte, trace *StructuredDecisionTrace) {
	if trace.StabilizationEffect == nil {
		return
	}

	*out = append(*out, "\n  Stabilization effect:\n"...)
	if trace.StabilizationEffect.WindowSeconds > 0 {
		*out = fmt.Appendf(*out, "    window: %ds\n", trace.StabilizationEffect.WindowSeconds)
	}
	if trace.StabilizationEffect.Direction != "" {
		*out = fmt.Appendf(*out, "    direction: %s\n", trace.StabilizationEffect.Direction)
	}
	if trace.StabilizationEffect.RemainingSeconds != nil {
		*out = fmt.Appendf(*out, "    remaining: ~%ds\n", *trace.StabilizationEffect.RemainingSeconds)
	}
	if trace.StabilizationEffect.Note != "" {
		*out = fmt.Appendf(*out, "    %s\n", trace.StabilizationEffect.Note)
	}
}

// appendDecisionTraceDecisionPath renders the ordered decision path steps.
func appendDecisionTraceDecisionPath(out *[]byte, trace *StructuredDecisionTrace) {
	if len(trace.DecisionPath) == 0 {
		return
	}

	*out = append(*out, "\n  Decision path:\n"...)
	for _, step := range trace.DecisionPath {
		*out = fmt.Appendf(*out, "    %d. %s\n", step.Step, step.Description)
		*out = fmt.Appendf(*out, "       result: %s\n", step.Result)
		if step.Impact != "" {
			*out = fmt.Appendf(*out, "       impact: %s\n", step.Impact)
		}
		if step.Confidence != "" {
			*out = fmt.Appendf(*out, "       confidence: %s\n", string(step.Confidence))
		}
	}
}
