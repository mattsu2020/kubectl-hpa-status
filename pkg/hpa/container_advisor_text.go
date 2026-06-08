package hpa

import (
	"fmt"
	"io"
)

// WriteContainerAdvisorText renders a ContainerAdvisorResult as human-readable text.
func WriteContainerAdvisorText(w io.Writer, result *ContainerAdvisorResult, provider LabelProvider) error {
	if result == nil {
		return nil
	}

	labels := resolveLabels(provider)

	if _, err := fmt.Fprintf(w, "%s:\n", labels.ContainerAdvisor); err != nil {
		return err
	}

	// Finding
	if _, err := fmt.Fprintf(w, "  Finding:\n    %s\n", result.Finding); err != nil {
		return err
	}

	// Risk
	if result.Risk != "" {
		if _, err := fmt.Fprintf(w, "\n  Risk:\n    %s\n", result.Risk); err != nil {
			return err
		}
	}

	// Suggested metric
	if result.SuggestedMetric != "" {
		if _, err := fmt.Fprintf(w, "\n  Suggested HPA metric:\n"); err != nil {
			return err
		}
		for _, line := range splitLines(result.SuggestedMetric) {
			if _, err := fmt.Fprintf(w, "    %s\n", line); err != nil {
				return err
			}
		}
	}

	// Confidence
	if _, err := fmt.Fprintf(w, "\n  Confidence: %s\n", string(result.Confidence)); err != nil {
		return err
	}

	// Container usage hints
	if len(result.ContainerUsageHints) > 0 {
		if _, err := fmt.Fprintf(w, "\n  Container usage:\n"); err != nil {
			return err
		}
		for _, h := range result.ContainerUsageHints {
			dominant := ""
			if h.Dominant {
				dominant = " (dominant)"
			}
			cpuStr := "unknown"
			if h.CPUPercent >= 0 {
				cpuStr = fmt.Sprintf("%d%%", h.CPUPercent)
			}
			memStr := "unknown"
			if h.MemoryPercent >= 0 {
				memStr = fmt.Sprintf("%d%%", h.MemoryPercent)
			}
			if _, err := fmt.Fprintf(w, "    %s%s: cpu=%s, memory=%s\n", h.Container, dominant, cpuStr, memStr); err != nil {
				return err
			}
		}
	}

	// Next action
	if result.NextAction != "" {
		if _, err := fmt.Fprintf(w, "\n  Next action:\n    %s\n", result.NextAction); err != nil {
			return err
		}
	}

	return nil
}

// AppendContainerAdvisorText appends the container advisor section to a byte buffer
// (used by the main status text renderer).
func AppendContainerAdvisorText(out *[]byte, result *ContainerAdvisorResult, labels labels) {
	if result == nil {
		return
	}

	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.ContainerAdvisor)

	*out = fmt.Appendf(*out, "  Finding:\n    %s\n", result.Finding)

	if result.Risk != "" {
		*out = fmt.Appendf(*out, "\n  Risk:\n    %s\n", result.Risk)
	}

	if result.SuggestedMetric != "" {
		*out = append(*out, "\n  Suggested HPA metric:\n"...)
		for _, line := range splitLines(result.SuggestedMetric) {
			*out = fmt.Appendf(*out, "    %s\n", line)
		}
	}

	*out = fmt.Appendf(*out, "\n  Confidence: %s\n", string(result.Confidence))

	if len(result.ContainerUsageHints) > 0 {
		*out = append(*out, "\n  Container usage:\n"...)
		for _, h := range result.ContainerUsageHints {
			dominant := ""
			if h.Dominant {
				dominant = " (dominant)"
			}
			cpuStr := "unknown"
			if h.CPUPercent >= 0 {
				cpuStr = fmt.Sprintf("%d%%", h.CPUPercent)
			}
			memStr := "unknown"
			if h.MemoryPercent >= 0 {
				memStr = fmt.Sprintf("%d%%", h.MemoryPercent)
			}
			*out = fmt.Appendf(*out, "    %s%s: cpu=%s, memory=%s\n", h.Container, dominant, cpuStr, memStr)
		}
	}

	if result.NextAction != "" {
		*out = fmt.Appendf(*out, "\n  Next action:\n    %s\n", result.NextAction)
	}
}

// splitLines splits a string by newlines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
