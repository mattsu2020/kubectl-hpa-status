package hpa

import (
	"fmt"
	"io"
)

// WriteAuditText renders an AuditReport as human-readable text.
func WriteAuditText(w io.Writer, report *AuditReport, provider LabelProvider) error {
	labels := resolveLabels(provider)

	if _, err := fmt.Fprintf(w, "%s: %s/%s (%s)\n", labels.Target, report.Namespace, report.Name, report.Target); err != nil {
		return err
	}
	if report.Profile != "" {
		if _, err := fmt.Fprintf(w, "Profile: %s\n", report.Profile); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%s: %d/100\n", labels.AuditScore, report.Score); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s\n\n", report.Summary); err != nil {
		return err
	}

	if len(report.Findings) == 0 {
		if _, err := fmt.Fprintln(w, "No findings."); err != nil {
			return err
		}
		return nil
	}

	for i, f := range report.Findings {
		if err := writeAuditFinding(w, i+1, f); err != nil {
			return err
		}
	}
	return nil
}

func writeAuditFinding(w io.Writer, index int, f AuditFinding) error {
	severity := string(f.Severity)
	if _, err := fmt.Fprintf(w, "%d. [%s] %s (%s)\n", index, severity, f.Title, f.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "   %s\n", f.Description); err != nil {
		return err
	}
	if f.Current != "" {
		if _, err := fmt.Fprintf(w, "   Current: %s", f.Current); err != nil {
			return err
		}
		if f.Recommended != "" {
			if _, err := fmt.Fprintf(w, " → Recommended: %s", f.Recommended); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if f.Command != "" {
		if _, err := fmt.Fprintf(w, "   Command: %s\n", f.Command); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return nil
}
