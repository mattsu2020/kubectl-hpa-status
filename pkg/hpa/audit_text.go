package hpa

import (
	"fmt"
	"io"
)

// WriteAuditText renders an AuditReport as human-readable text.
func WriteAuditText(w io.Writer, report *AuditReport, provider LabelProvider) error {
	labels := resolveLabels(provider)

	fmt.Fprintf(w, "%s: %s/%s (%s)\n", labels.Target, report.Namespace, report.Name, report.Target)
	fmt.Fprintf(w, "%s: %d/100\n", labels.AuditScore, report.Score)
	fmt.Fprintf(w, "%s\n\n", report.Summary)

	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "No findings.")
		return nil
	}

	for i, f := range report.Findings {
		severity := string(f.Severity)
		fmt.Fprintf(w, "%d. [%s] %s (%s)\n", i+1, severity, f.Title, f.ID)
		fmt.Fprintf(w, "   %s\n", f.Description)
		if f.Current != "" {
			fmt.Fprintf(w, "   Current: %s", f.Current)
			if f.Recommended != "" {
				fmt.Fprintf(w, " → Recommended: %s", f.Recommended)
			}
			fmt.Fprintln(w)
		}
		if f.Command != "" {
			fmt.Fprintf(w, "   Command: %s\n", f.Command)
		}
		fmt.Fprintln(w)
	}
	return nil
}
