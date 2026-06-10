package hpa

import (
	"fmt"
)

// AppendFlappingPreventionText appends the flapping prevention advisor
// section to a byte buffer (used by the main status text renderer).
func AppendFlappingPreventionText(out *[]byte, report *FlappingPreventionReport, labels labels) {
	if report == nil {
		return
	}

	label := labels.FlappingPrevention
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", label)
	*out = fmt.Appendf(*out, "  Current stabilization window: %ds\n", report.CurrentWindow)
	*out = fmt.Appendf(*out, "  Direction flips: %d\n", report.CurrentDirectionFlips)
	*out = fmt.Appendf(*out, "  Observation window: %s\n", report.ObservationWindow)

	if len(report.Recommendations) > 0 {
		*out = append(*out, "\n  Recommendations:\n"...)
		for _, rec := range report.Recommendations {
			*out = fmt.Appendf(*out, "    Window: %ds | Flips: %d -> %d (%.0f%% reduction) | Confidence: %s\n",
				rec.WindowSeconds, report.CurrentDirectionFlips, rec.EstimatedDirectionFlips,
				rec.EstimatedFlapReduction, rec.Confidence)
			*out = fmt.Appendf(*out, "      %s\n", rec.Rationale)
			if rec.Patch != "" {
				*out = fmt.Appendf(*out, "      Patch: %s\n", rec.Patch)
			}
		}
	}

	*out = fmt.Appendf(*out, "\n  %s\n", report.Summary)
}
