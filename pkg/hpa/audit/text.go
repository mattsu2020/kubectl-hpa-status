package audit

import (
	"fmt"
	"io"
)

// LabelProvider abstracts localized label lookup for audit rendering. It
// mirrors the root hpaanalysis.LabelProvider contract (a single Get method),
// so callers can pass any i18n provider without this package importing the
// analysis root.
type LabelProvider interface {
	Get(key string) string
}

// labelText resolves a localized label through the provider, falling back to
// the English default when no provider is configured or the lookup is empty.
func labelText(provider LabelProvider, key, fallback string) string {
	if provider != nil {
		if value := provider.Get(key); value != "" {
			return value
		}
	}
	return fallback
}

// WriteText renders a Report as human-readable text.
func WriteText(w io.Writer, report *Report, provider LabelProvider) error {
	target := labelText(provider, "label_target", "Target")

	if _, err := fmt.Fprintf(w, "%s: %s/%s (%s)\n", target, report.Namespace, report.Name, report.Target); err != nil {
		return err
	}
	if report.Profile != "" {
		if _, err := fmt.Fprintf(w, "Profile: %s\n", report.Profile); err != nil {
			return err
		}
	}
	score := labelText(provider, "label_audit_score", "Compliance Score")
	if _, err := fmt.Fprintf(w, "%s: %d/100\n", score, report.Score); err != nil {
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
		if err := writeFinding(w, i+1, f); err != nil {
			return err
		}
	}
	return nil
}

func writeFinding(w io.Writer, index int, f Finding) error {
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
