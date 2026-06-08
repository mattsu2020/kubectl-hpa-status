package hpa

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

// WriteChurnText renders a ChurnAnalysis as formatted terminal output.
func WriteChurnText(w io.Writer, analysis *ChurnAnalysis, theme style.Theme) error {
	if analysis == nil {
		return nil
	}

	var out strings.Builder

	levelStyle := churnLevelStyle(analysis.Level, theme)
	out.WriteString(fmt.Sprintf("Churn Score: %s/100 (%s)\n",
		levelStyle.Render(fmt.Sprintf("%d", analysis.Score)),
		levelStyle.Render(string(analysis.Level))))
	out.WriteString(fmt.Sprintf("  Scale-up: %d | Scale-down: %d | Direction flips: %d\n",
		analysis.ScaleUpCount, analysis.ScaleDownCount, analysis.DirectionFlips))
	out.WriteString(fmt.Sprintf("  Avg replica delta: ±%.1f | Max delta: %d\n",
		analysis.AvgReplicaDelta, analysis.MaxReplicaDelta))
	if analysis.TimeWindow > 0 {
		out.WriteString(fmt.Sprintf("  Time window: %s\n", analysis.TimeWindow.Round(time.Minute)))
	}

	if len(analysis.Recommendations) > 0 {
		out.WriteString("\nChurn Recommendations:\n")
		for _, rec := range analysis.Recommendations {
			out.WriteString(fmt.Sprintf("  [%s] %s: %s → %s\n",
				rec.Type, rec.Rationale, rec.CurrentValue, rec.RecommendedValue))
			if rec.Patch != "" {
				out.WriteString(fmt.Sprintf("    Patch: %s\n", rec.Patch))
			}
		}
	}

	_, err := fmt.Fprint(w, out.String())
	return err
}

// WriteChurnMarkdown renders a ChurnAnalysis as a Markdown section.
func WriteChurnMarkdown(w io.Writer, analysis *ChurnAnalysis) error {
	if analysis == nil {
		return nil
	}

	var out strings.Builder

	out.WriteString("## Churn Analysis\n\n")
	out.WriteString(fmt.Sprintf("- **Score:** %d/100 (%s)\n", analysis.Score, analysis.Level))
	out.WriteString(fmt.Sprintf("- **Scale-up:** %d | **Scale-down:** %d | **Direction flips:** %d\n",
		analysis.ScaleUpCount, analysis.ScaleDownCount, analysis.DirectionFlips))
	out.WriteString(fmt.Sprintf("- **Avg replica delta:** ±%.1f | **Max delta:** %d\n",
		analysis.AvgReplicaDelta, analysis.MaxReplicaDelta))
	if analysis.TimeWindow > 0 {
		out.WriteString(fmt.Sprintf("- **Time window:** %s\n", analysis.TimeWindow.Round(time.Minute)))
	}

	if len(analysis.Recommendations) > 0 {
		out.WriteString("\n### Recommendations\n\n")
		for _, rec := range analysis.Recommendations {
			out.WriteString(fmt.Sprintf("- **%s** (%s): %s → %s\n", rec.Type, rec.Confidence, rec.CurrentValue, rec.RecommendedValue))
			out.WriteString(fmt.Sprintf("  - %s\n", rec.Rationale))
		}
	}

	_, err := fmt.Fprint(w, out.String())
	return err
}

// WriteChurnHTML renders a ChurnAnalysis as an HTML section.
func WriteChurnHTML(w io.Writer, analysis *ChurnAnalysis) error {
	if analysis == nil {
		return nil
	}

	var out strings.Builder

	out.WriteString(`<div class="churn-analysis">`)
	out.WriteString(`<h3>Churn Analysis</h3>`)
	out.WriteString(fmt.Sprintf(`<p><span class="churn-%s">Score: %d/100 (%s)</span></p>`,
		strings.ToLower(string(analysis.Level)), analysis.Score, analysis.Level))
	out.WriteString(fmt.Sprintf(`<p>Scale-up: %d | Scale-down: %d | Direction flips: %d</p>`,
		analysis.ScaleUpCount, analysis.ScaleDownCount, analysis.DirectionFlips))

	if len(analysis.Recommendations) > 0 {
		out.WriteString(`<h4>Recommendations</h4><ul>`)
		for _, rec := range analysis.Recommendations {
			out.WriteString(fmt.Sprintf(`<li><strong>%s</strong>: %s → %s — %s</li>`,
				htmlEscape(rec.Type), htmlEscape(rec.CurrentValue), htmlEscape(rec.RecommendedValue), htmlEscape(rec.Rationale)))
		}
		out.WriteString(`</ul>`)
	}
	out.WriteString(`</div>`)

	_, err := fmt.Fprint(w, out.String())
	return err
}

func churnLevelStyle(level ChurnLevel, theme style.Theme) lipgloss.Style {
	switch level {
	case ChurnLow:
		return theme.OK
	case ChurnMedium:
		return theme.Warning
	case ChurnHigh, ChurnCritical:
		return theme.Error
	default:
		return theme.Dim
	}
}
