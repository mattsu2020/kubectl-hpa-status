package hpa

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// WriteAssumptionsText renders ControllerAssumptions as a formatted table.
func WriteAssumptionsText(w io.Writer, assumptions *ControllerAssumptions, theme style.Theme) error {
	if assumptions == nil {
		return nil
	}

	var out strings.Builder

	out.WriteString(fmt.Sprintf("HPA Controller Assumptions: %s/%s\n\n",
		assumptions.Namespace, assumptions.Name))

	assumptionRows := []struct {
		label string
		a     Assumption
	}{
		{"Sync Period", assumptions.SyncPeriod},
		{"Global Tolerance", assumptions.GlobalTolerance},
		{"CPU Init Period", assumptions.CPUInitializationPeriod},
		{"Initial Readiness Delay", assumptions.InitialReadinessDelay},
		{"Downscale Stabilization", assumptions.DownscaleStabilization},
		{"Upscale Stabilization", assumptions.UpscaleStabilization},
	}

	out.WriteString(fmt.Sprintf("%-26s %-14s %-20s %-10s %s\n",
		"ASSUMPTION", "VALUE", "SOURCE", "CONFIDENCE", "IMPACT"))
	out.WriteString(strings.Repeat("-", 100) + "\n")

	for _, row := range assumptionRows {
		cs := confidenceStyle(row.a.Confidence, theme)
		out.WriteString(fmt.Sprintf("%-26s %-14s %-20s %-10s %s\n",
			row.label,
			row.a.Value,
			row.a.Source,
			cs.Render(row.a.Confidence),
			truncateImpact(row.a.Impact, 40)))
	}

	if assumptions.Summary != "" {
		out.WriteString(fmt.Sprintf("\n%s\n", assumptions.Summary))
	}

	if len(assumptions.Warnings) > 0 {
		out.WriteString("\nImpact:\n")
		for _, warning := range assumptions.Warnings {
			out.WriteString(fmt.Sprintf("  - %s\n", warning))
		}
	}

	out.WriteString(fmt.Sprintf("\nConfidence:\n  %s: derived from Kubernetes defaults and visible HPA spec\n",
		assumptionsSummary(assumptions)))
	out.WriteString("  low: actual kube-controller-manager flags are not visible via HPA API\n")

	_, err := fmt.Fprint(w, out.String())
	return err
}

// WriteAssumptionsMarkdown renders ControllerAssumptions as a Markdown table.
func WriteAssumptionsMarkdown(w io.Writer, assumptions *ControllerAssumptions) error {
	if assumptions == nil {
		return nil
	}

	var out strings.Builder

	out.WriteString(fmt.Sprintf("## HPA Controller Assumptions: %s/%s\n\n",
		assumptions.Namespace, assumptions.Name))

	out.WriteString("| Assumption | Value | Source | Confidence |\n")
	out.WriteString("|---|---|---|---|\n")

	assumptionRows := []struct {
		label string
		a     Assumption
	}{
		{"Sync Period", assumptions.SyncPeriod},
		{"Global Tolerance", assumptions.GlobalTolerance},
		{"CPU Init Period", assumptions.CPUInitializationPeriod},
		{"Initial Readiness Delay", assumptions.InitialReadinessDelay},
		{"Downscale Stabilization", assumptions.DownscaleStabilization},
		{"Upscale Stabilization", assumptions.UpscaleStabilization},
	}

	for _, row := range assumptionRows {
		out.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			row.label, row.a.Value, row.a.Source, row.a.Confidence))
	}

	if len(assumptions.Warnings) > 0 {
		out.WriteString("\n### Impact\n\n")
		for _, warning := range assumptions.Warnings {
			out.WriteString(fmt.Sprintf("- %s\n", warning))
		}
	}

	_, err := fmt.Fprint(w, out.String())
	return err
}

func confidenceStyle(confidence string, theme style.Theme) lipgloss.Style {
	switch confidence {
	case "high", "overridden":
		return theme.OK
	case "medium":
		return theme.Warning
	case "low":
		return theme.Dim
	default:
		return theme.Dim
	}
}

func truncateImpact(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func assumptionsSummary(a *ControllerAssumptions) string {
	high, medium, low, overridden := 0, 0, 0, 0
	for _, row := range []Assumption{
		a.SyncPeriod, a.GlobalTolerance, a.CPUInitializationPeriod,
		a.InitialReadinessDelay, a.DownscaleStabilization, a.UpscaleStabilization,
	} {
		switch row.Confidence {
		case "high":
			high++
		case "medium":
			medium++
		case "low":
			low++
		case "overridden":
			overridden++
		}
	}
	parts := []string{fmt.Sprintf("high=%d", high), fmt.Sprintf("medium=%d", medium), fmt.Sprintf("low=%d", low)}
	if overridden > 0 {
		parts = append(parts, fmt.Sprintf("overridden=%d", overridden))
	}
	return strings.Join(parts, ", ")
}

// WriteAssumptionsTextWithExplain renders ControllerAssumptions with optional
// detailed explanations for each assumption. When explain is true, each
// assumption includes its Description and a contextual warning block.
func WriteAssumptionsTextWithExplain(w io.Writer, assumptions *ControllerAssumptions, explain bool, theme style.Theme) error {
	if assumptions == nil {
		return nil
	}

	var out strings.Builder

	out.WriteString(fmt.Sprintf("HPA Controller Assumptions: %s/%s\n\n",
		assumptions.Namespace, assumptions.Name))

	assumptionRows := []struct {
		label string
		a     Assumption
	}{
		{"Sync Period", assumptions.SyncPeriod},
		{"Global Tolerance", assumptions.GlobalTolerance},
		{"CPU Init Period", assumptions.CPUInitializationPeriod},
		{"Initial Readiness Delay", assumptions.InitialReadinessDelay},
		{"Downscale Stabilization", assumptions.DownscaleStabilization},
		{"Upscale Stabilization", assumptions.UpscaleStabilization},
	}

	if explain {
		out.WriteString(fmt.Sprintf("%-26s %-14s %-20s %-12s %s\n",
			"ASSUMPTION", "VALUE", "SOURCE", "CONFIDENCE", "IMPACT"))
		out.WriteString(strings.Repeat("-", 110) + "\n")
		for _, row := range assumptionRows {
			cs := confidenceStyle(row.a.Confidence, theme)
			out.WriteString(fmt.Sprintf("%-26s %-14s %-20s %-12s %s\n",
				row.label, row.a.Value, row.a.Source, cs.Render(row.a.Confidence), truncateImpact(row.a.Impact, 40)))
			if row.a.Description != "" {
				out.WriteString(fmt.Sprintf("  %s\n", theme.Dim.Render(row.a.Description)))
			}
		}
	} else {
		out.WriteString(fmt.Sprintf("%-26s %-14s %-20s %-10s %s\n",
			"ASSUMPTION", "VALUE", "SOURCE", "CONFIDENCE", "IMPACT"))
		out.WriteString(strings.Repeat("-", 100) + "\n")
		for _, row := range assumptionRows {
			cs := confidenceStyle(row.a.Confidence, theme)
			out.WriteString(fmt.Sprintf("%-26s %-14s %-20s %-10s %s\n",
				row.label, row.a.Value, row.a.Source, cs.Render(row.a.Confidence), truncateImpact(row.a.Impact, 40)))
		}
	}

	if assumptions.Summary != "" {
		out.WriteString(fmt.Sprintf("\n%s\n", assumptions.Summary))
	}

	if len(assumptions.Warnings) > 0 {
		out.WriteString("\nImpact:\n")
		for _, warning := range assumptions.Warnings {
			out.WriteString(fmt.Sprintf("  - %s\n", warning))
		}
	}

	if explain {
		writeAssumptionsLowConfidenceNotes(&out, assumptionRows)
	}

	out.WriteString(fmt.Sprintf("\nConfidence:\n  %s: derived from Kubernetes defaults and visible HPA spec\n",
		assumptionsSummary(assumptions)))
	out.WriteString("  low: actual kube-controller-manager flags are not visible via HPA API\n")

	_, err := fmt.Fprint(w, out.String())
	return err
}

// writeAssumptionsLowConfidenceNotes appends the low-confidence warning and any
// observed kube-system controller-manager profile note (explain mode only).
func writeAssumptionsLowConfidenceNotes(out *strings.Builder, rows []struct {
	label string
	a     Assumption
}) {
	hasLow := false
	for _, row := range rows {
		if row.a.Confidence == "low" {
			hasLow = true
			break
		}
	}
	if hasLow {
		out.WriteString("\nWarning:\n")
		out.WriteString("  kube-controller-manager flags are not directly visible via the HPA API.\n")
		out.WriteString("  Values marked 'low' confidence may differ in your cluster.\n")
	}
	for _, row := range rows {
		if strings.HasPrefix(row.a.Source, "kube-system/") {
			out.WriteString(fmt.Sprintf("\nObserved profile: %s\n", row.a.Source))
			break
		}
	}
}
