package hpa

import (
	"fmt"
	"io"
	"math"

	"charm.land/lipgloss/v2"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/warmup"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// AppendWarmupText writes the warmup analysis section to out.
func AppendWarmupText(out *[]byte, analysis *warmup.Analysis, theme style.Theme, lbls labels) {
	if analysis == nil {
		return
	}

	summaryLabel := theme.SummaryColor(analysis.Summary)
	*out = fmt.Appendf(*out, "\n%s: %s\n", lbls.Warmup, summaryLabel)

	// HPA decision section.
	*out = append(*out, "  HPA decision:\n"...)
	*out = fmt.Appendf(*out, "    desiredReplicas: %d\n", analysis.DesiredReplicas)
	*out = fmt.Appendf(*out, "    currentReplicas: %d\n", analysis.CurrentReplicas)
	*out = fmt.Appendf(*out, "    readyPods: %d\n", analysis.ReadyPods)
	*out = fmt.Appendf(*out, "    availablePods: %d\n", analysis.AvailablePods)

	// Bottleneck section.
	if len(analysis.Bottlenecks) > 0 {
		*out = append(*out, "\n  Likely bottleneck:\n"...)
		for _, b := range analysis.Bottlenecks {
			severityStyle := bottleneckSeverityStyle(b.Severity, theme)
			*out = fmt.Appendf(*out, "    %s %s\n",
				severityStyle.Render(fmt.Sprintf("[%s]", b.Type)),
				b.Message)
		}
	}

	// Evidence section.
	if len(analysis.Evidence) > 0 {
		*out = append(*out, "\n  Evidence:\n"...)
		for _, e := range analysis.Evidence {
			*out = fmt.Appendf(*out, "    - %s\n", e)
		}
	}

	// Impact section.
	if analysis.Impact != "" {
		pct := int(math.Round(analysis.EffectiveCapacityRatio * 100))
		pctLabel := theme.SummaryColor(fmt.Sprintf("%d%%", pct))
		*out = fmt.Appendf(*out, "\n  Impact:\n    %s (effective capacity: %s)\n",
			analysis.Impact, pctLabel)
	}

	// Recommended actions section.
	if len(analysis.RecommendedActions) > 0 {
		*out = append(*out, "\n  Recommended actions:\n"...)
		for _, a := range analysis.RecommendedActions {
			*out = fmt.Appendf(*out, "    - %s\n", theme.InterpretationLine(a))
		}
	}
}

// WriteWarmupText writes a standalone warmup report.
func WriteWarmupText(w io.Writer, analysis *warmup.Analysis, theme style.Theme) error {
	if analysis == nil {
		return nil
	}
	var out []byte
	lbls := resolveLabels(nil)
	AppendWarmupText(&out, analysis, theme, lbls)
	_, err := w.Write(out)
	return err
}

// WriteWarmupMarkdown renders a WarmupAnalysis as a Markdown section.
func WriteWarmupMarkdown(w io.Writer, analysis *warmup.Analysis) error {
	if analysis == nil {
		return nil
	}

	var out []byte
	out = fmt.Appendf(out, "## Warmup Analysis\n\n")
	out = fmt.Appendf(out, "- **Summary:** %s\n", analysis.Summary)
	out = fmt.Appendf(out, "- **Effective capacity:** %.0f%% (%d/%d pods ready)\n",
		analysis.EffectiveCapacityRatio*100, analysis.ReadyPods, analysis.DesiredReplicas)
	out = fmt.Appendf(out, "- **Avg time-to-ready:** %ds | **P95:** %ds\n",
		analysis.AvgTimeToReadySeconds, analysis.P95TimeToReadySeconds)

	if len(analysis.Bottlenecks) > 0 {
		out = append(out, "\n### Bottlenecks\n\n"...)
		for _, b := range analysis.Bottlenecks {
			out = fmt.Appendf(out, "- **%s** (%s, %s): %s\n", b.Type, b.Severity, b.Confidence, b.Message)
		}
	}

	if len(analysis.RecommendedActions) > 0 {
		out = append(out, "\n### Recommended Actions\n\n"...)
		for _, a := range analysis.RecommendedActions {
			out = fmt.Appendf(out, "- %s\n", a)
		}
	}

	_, err := w.Write(out)
	return err
}

// WriteWarmupHTML renders a WarmupAnalysis as an HTML section.
func WriteWarmupHTML(w io.Writer, analysis *warmup.Analysis) error {
	if analysis == nil {
		return nil
	}

	var out []byte
	out = append(out, `<div class="warmup-analysis">`...)
	out = append(out, `<h3>Warmup Analysis</h3>`...)
	out = fmt.Appendf(out, `<p><span class="warmup-%s">Summary: %s</span></p>`,
		htmlEscape(analysis.Summary), htmlEscape(analysis.Summary))
	out = fmt.Appendf(out, `<p>Effective capacity: %.0f%% (%d/%d pods ready)</p>`,
		analysis.EffectiveCapacityRatio*100, analysis.ReadyPods, analysis.DesiredReplicas)

	if len(analysis.Bottlenecks) > 0 {
		out = append(out, `<h4>Bottlenecks</h4><ul>`...)
		for _, b := range analysis.Bottlenecks {
			out = fmt.Appendf(out, `<li><strong>%s</strong> (%s): %s</li>`,
				htmlEscape(b.Type), htmlEscape(string(b.Severity)), htmlEscape(b.Message))
		}
		out = append(out, `</ul>`...)
	}

	if len(analysis.RecommendedActions) > 0 {
		out = append(out, `<h4>Recommended Actions</h4><ul>`...)
		for _, a := range analysis.RecommendedActions {
			out = fmt.Appendf(out, `<li>%s</li>`, htmlEscape(a))
		}
		out = append(out, `</ul>`...)
	}

	out = append(out, `</div>`...)
	_, err := w.Write(out)
	return err
}

func bottleneckSeverityStyle(severity Severity, theme style.Theme) lipgloss.Style {
	switch severity {
	case SeverityError:
		return theme.Error
	case SeverityWarning:
		return theme.Warning
	default:
		return theme.Dim
	}
}
