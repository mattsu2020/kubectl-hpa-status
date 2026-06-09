package hpa

import (
	"fmt"
	"strings"
)

// FormatDecisionSignals renders a list of DecisionSignal entries as
// human-readable text for --explain mode output.
func FormatDecisionSignals(signals []DecisionSignal) string {
	if len(signals) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "Decision Signals:")
	for _, sig := range signals {
		confidence := sig.Confidence
		if confidence == "" {
			confidence = "unknown"
		}
		source := sig.Source
		if source == "" {
			source = "unknown"
		}

		var metricPart string
		if sig.MetricName != "" {
			metricPart = fmt.Sprintf(" metric=%s", sig.MetricName)
		}

		adapterPart := ""
		if sig.AdapterVersion != "" {
			adapterPart = fmt.Sprintf(" [%s]", sig.AdapterVersion)
		}

		line := fmt.Sprintf("  - %s: %s%s (source: %s, confidence: %s%s)",
			sig.Reason, sig.Message, metricPart, source, confidence, adapterPart)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// FormatDecisionSignalsCompact renders a compact single-line summary of
// decision signals for status displays.
func FormatDecisionSignalsCompact(signals []DecisionSignal) string {
	if len(signals) == 0 {
		return ""
	}

	parts := make([]string, 0, len(signals))
	for _, sig := range signals {
		parts = append(parts, sig.Reason)
	}

	return fmt.Sprintf("decisions: %s", strings.Join(parts, ", "))
}
