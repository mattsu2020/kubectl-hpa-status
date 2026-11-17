package backtest

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// WriteText renders a human-readable backtest ranking table and recommendations.
func WriteText(w io.Writer, report *Report) error {
	if report == nil {
		return fmt.Errorf("backtest report is nil")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("HPA Backtest: %s/%s  snapshots=%d  metric-backed=%d  confidence=%s\n",
		report.Namespace, report.Name, report.Snapshots, report.MetricBackedSnapshots, report.Confidence))
	if report.Source != "" {
		sb.WriteString(fmt.Sprintf("Source: %s\n", report.Source))
	}
	sb.WriteString(fmt.Sprintf("Baseline: scaleEvents=%d  flips=%d  peak=%d  podHours=%.2f\n\n",
		report.Baseline.ScaleEvents, report.Baseline.DirectionFlips, report.Baseline.PeakReplicas, report.Baseline.PodHours))

	sb.WriteString(fmt.Sprintf("%-4s %-24s %-6s %-7s %-6s %-6s %-8s %-10s %s\n",
		"RANK", "CANDIDATE", "SCORE", "RISK", "FLIPS", "UNDER", "PEAK", "POD-HOURS", "RECOMMENDATION"))
	sb.WriteString(strings.Repeat("-", 104) + "\n")
	for _, c := range report.Candidates {
		sb.WriteString(fmt.Sprintf("%-4d %-24s %-6d %-7s %-6d %-6d %-8d %-10.2f %s\n",
			c.Rank,
			rendutil.TruncateDisplayWidth(c.Name, 24, "..."),
			c.HealthScore,
			c.RiskLevel,
			c.Summary.DirectionFlips,
			c.Summary.UnderProvisionedSnapshots,
			c.Summary.PeakReplicas,
			c.Summary.PodHours,
			c.Recommendation,
		))
	}
	if len(report.Limitations) > 0 {
		sb.WriteString("\nLimitations:\n")
		for _, lim := range report.Limitations {
			sb.WriteString(fmt.Sprintf("  - %s\n", lim))
		}
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

// WriteMarkdown renders the backtest ranking report as Markdown.
func WriteMarkdown(w io.Writer, report *Report) error {
	if report == nil {
		return fmt.Errorf("backtest report is nil")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# HPA Backtest: %s/%s\n\n",
		rendutil.MarkdownInline(report.Namespace), rendutil.MarkdownInline(report.Name)))
	sb.WriteString(fmt.Sprintf("- **Snapshots:** %d (%d metric-backed)\n", report.Snapshots, report.MetricBackedSnapshots))
	sb.WriteString(fmt.Sprintf("- **Confidence:** %s\n", rendutil.MarkdownInline(report.Confidence)))
	if report.BestCandidate != "" {
		sb.WriteString(fmt.Sprintf("- **Best Candidate:** %s\n", rendutil.MarkdownInline(report.BestCandidate)))
	}
	sb.WriteString("\n| Rank | Candidate | Score | Risk | Flips | Under-provisioned | Peak | Pod-hours |\n")
	sb.WriteString("| ---: | --- | ---: | --- | ---: | ---: | ---: | ---: |\n")
	for _, c := range report.Candidates {
		sb.WriteString(fmt.Sprintf("| %d | %s | %d | %s | %d | %d | %d | %.2f |\n",
			c.Rank,
			rendutil.MarkdownCell(c.Name),
			c.HealthScore,
			rendutil.MarkdownCell(c.RiskLevel),
			c.Summary.DirectionFlips,
			c.Summary.UnderProvisionedSnapshots,
			c.Summary.PeakReplicas,
			c.Summary.PodHours,
		))
	}
	if len(report.Limitations) > 0 {
		sb.WriteString("\n## Limitations\n\n")
		for _, lim := range report.Limitations {
			sb.WriteString(fmt.Sprintf("- %s\n", rendutil.MarkdownInline(lim)))
		}
	}
	_, err := io.WriteString(w, sb.String())
	return err
}
