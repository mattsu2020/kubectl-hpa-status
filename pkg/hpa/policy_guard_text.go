package hpa

import (
	"fmt"
	"io"
)

// WritePolicyGuardText writes a compact human-readable policy guard report.
func WritePolicyGuardText(w io.Writer, result *GuardResult) error {
	if result == nil {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nPolicy guard:"); err != nil {
		return err
	}
	for _, blocked := range result.Blocked {
		if _, err := fmt.Fprintf(w, "  - BLOCKED %s: %s (%s)\n", blocked.Suggestion.Title, blocked.Reason, blocked.PolicyRule); err != nil {
			return err
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(w, "  - WARNING %s: %s (%s)\n", warning.Suggestion.Title, warning.Reason, warning.PolicyRule); err != nil {
			return err
		}
	}
	if len(result.Blocked) == 0 && len(result.Warnings) == 0 {
		_, err := fmt.Fprintln(w, "  - All suggested patches passed.")
		return err
	}
	return nil
}
