package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
)

// WriteReadinessDoctorText renders a ReadinessDoctorReport as plain text.
func WriteReadinessDoctorText(w io.Writer, report *ReadinessDoctorReport, theme style.Theme) error {
	if report == nil {
		return nil
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("Readiness doctor: %s/%s (%s)\n\n",
		report.Namespace, report.Name, report.Target))

	buf.WriteString(fmt.Sprintf("Summary: %s\n\n", report.Summary))

	// Pod age distribution
	buf.WriteString("Pod age distribution:\n")
	dist := report.PodAgeDistribution
	buf.WriteString(fmt.Sprintf("  total: %d  young: %d  mature: %d\n",
		dist.TotalPods, dist.YoungPods, dist.MaturePods))
	if dist.YoungPods > 0 {
		buf.WriteString(fmt.Sprintf("  young pods: %d ready, %d not-ready\n",
			dist.ReadyYoungPods, dist.NotReadyYoungPods))
	}

	// Probe analysis
	buf.WriteString("\nProbe analysis:\n")
	pa := report.ProbeAnalysis
	buf.WriteString(fmt.Sprintf("  readinessProbe: %s\n", probePresentLabel(pa.HasReadinessProbe, theme)))
	buf.WriteString(fmt.Sprintf("  startupProbe: %s\n", probePresentLabel(pa.HasStartupProbe, theme)))
	if pa.ReadinessInitialDelaySec > 0 {
		buf.WriteString(fmt.Sprintf("  readinessProbe initialDelaySeconds: %d\n", pa.ReadinessInitialDelaySec))
	}
	buf.WriteString(fmt.Sprintf("  assessment: %s\n", pa.Assessment))
	for _, w := range pa.Warnings {
		buf.WriteString(fmt.Sprintf("  %s\n", theme.Dim.Render("- "+w)))
	}

	// Initialization impact
	buf.WriteString("\nCPU initialization window:\n")
	impact := report.InitializationImpact
	buf.WriteString(fmt.Sprintf("  CPU init period: %ds\n", impact.CPUInitPeriodSeconds))
	buf.WriteString(fmt.Sprintf("  estimated excluded pods: %d\n", impact.EstimatedExcludedPods))
	buf.WriteString(fmt.Sprintf("  %s\n", impact.ImpactDescription))

	// Exclusion estimate
	excl := report.ExclusionEstimate
	if excl.EstimatedExcludedCount > 0 {
		buf.WriteString("\nMetric exclusion estimate:\n")
		buf.WriteString(fmt.Sprintf("  not-ready pods: %d\n", excl.NotReadyPods))
		buf.WriteString(fmt.Sprintf("  missing metric pods: %d\n", excl.MissingMetricPods))
		buf.WriteString(fmt.Sprintf("  estimated excluded: %d\n", excl.EstimatedExcludedCount))
		buf.WriteString(fmt.Sprintf("  %s\n", excl.Explanation))
	}

	// Recommendations
	if len(report.Recommendations) > 0 {
		buf.WriteString("\nRecommendations:\n")
		for _, rec := range report.Recommendations {
			buf.WriteString(fmt.Sprintf("  %s %s\n", theme.ActionLine("-"), rec))
		}
	}

	// Next checks
	if len(report.NextChecks) > 0 {
		buf.WriteString("\nNext checks:\n")
		for _, check := range report.NextChecks {
			buf.WriteString(fmt.Sprintf("  - %s\n", check))
		}
	}

	_, err := w.Write([]byte(buf.String()))
	return err
}

// WriteReadinessDoctorMarkdown renders a ReadinessDoctorReport as Markdown.
func WriteReadinessDoctorMarkdown(w io.Writer, report *ReadinessDoctorReport) error {
	if report == nil {
		return nil
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("## Readiness Doctor: %s/%s (%s)\n\n",
		report.Namespace, report.Name, report.Target))

	buf.WriteString(fmt.Sprintf("**Summary:** %s\n\n", report.Summary))

	dist := report.PodAgeDistribution
	buf.WriteString("### Pod Age Distribution\n\n")
	buf.WriteString("| Metric | Value |\n|---|---|\n")
	buf.WriteString(fmt.Sprintf("| Total pods | %d |\n", dist.TotalPods))
	buf.WriteString(fmt.Sprintf("| Young (< %ds) | %d |\n", report.InitializationImpact.CPUInitPeriodSeconds, dist.YoungPods))
	buf.WriteString(fmt.Sprintf("| Mature | %d |\n", dist.MaturePods))
	if dist.YoungPods > 0 {
		buf.WriteString(fmt.Sprintf("| Young ready | %d |\n", dist.ReadyYoungPods))
		buf.WriteString(fmt.Sprintf("| Young not-ready | %d |\n", dist.NotReadyYoungPods))
	}
	buf.WriteString("\n")

	pa := report.ProbeAnalysis
	buf.WriteString("### Probe Analysis\n\n")
	buf.WriteString("| Probe | Present | Delay |\n|---|---|---|\n")
	buf.WriteString(fmt.Sprintf("| readinessProbe | %t | %ds |\n", pa.HasReadinessProbe, pa.ReadinessInitialDelaySec))
	buf.WriteString(fmt.Sprintf("| startupProbe | %t | - |\n\n", pa.HasStartupProbe))
	buf.WriteString(fmt.Sprintf("**Assessment:** %s\n\n", pa.Assessment))
	for _, w := range pa.Warnings {
		buf.WriteString(fmt.Sprintf("- %s\n", w))
	}
	buf.WriteString("\n")

	buf.WriteString("### CPU Initialization Window\n\n")
	impact := report.InitializationImpact
	buf.WriteString(fmt.Sprintf("- CPU init period: %ds\n", impact.CPUInitPeriodSeconds))
	buf.WriteString(fmt.Sprintf("- Estimated excluded pods: %d\n", impact.EstimatedExcludedPods))
	buf.WriteString(fmt.Sprintf("- %s\n\n", impact.ImpactDescription))

	if len(report.Recommendations) > 0 {
		buf.WriteString("### Recommendations\n\n")
		for _, rec := range report.Recommendations {
			buf.WriteString(fmt.Sprintf("- %s\n", rec))
		}
		buf.WriteString("\n")
	}

	_, err := w.Write([]byte(buf.String()))
	return err
}

// probePresentLabel returns a styled yes/no label for probe presence.
func probePresentLabel(present bool, theme style.Theme) string {
	if present {
		return theme.OK.Render("configured")
	}
	return theme.Warning.Render("not configured")
}
