package hpa

import (
	"fmt"
	"io"
	"strings"
)

// WriteLintText renders a LintResult as human-readable text.
func WriteLintText(w io.Writer, result *LintResult) error {
	if result == nil {
		return nil
	}

	if len(result.Findings) == 0 {
		_, err := fmt.Fprintln(w, "No issues found.")
		return err
	}

	for _, f := range result.Findings {
		prefix := string(f.Severity)
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", prefix, f.Rule, f.Message); err != nil {
			return err
		}
	}

	if err := WriteLintSummary(w, result); err != nil {
		return err
	}

	return nil
}

// WriteLintSummary renders a severity breakdown and pass/fail status.
func WriteLintSummary(w io.Writer, result *LintResult) error {
	if result == nil {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\n%d error(s), %d warning(s), %d info(s)\n",
		result.Errors, result.Warnings, result.Infos); err != nil {
		return err
	}

	status := "PASS"
	if !result.Pass {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "Status: %s\n", status); err != nil {
		return err
	}

	return nil
}

// WriteLintDiff renders auto-fix proposals for each finding that has one.
// For each fixable finding, it shows the rule, severity, before/after
// comparison, the kubectl patch command, and the risk level.
func WriteLintDiff(w io.Writer, result *LintResult) error {
	if result == nil {
		return nil
	}

	fixableCount := 0
	for _, f := range result.Findings {
		if f.AutoFix == nil {
			continue
		}
		fixableCount++

		if _, err := fmt.Fprintf(w, "\n--- [%s] %s ---\n", f.Severity, f.Rule); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  Message: %s\n", f.Message); err != nil {
			return err
		}
		if f.Recommendation != "" {
			if _, err := fmt.Fprintf(w, "  Recommendation: %s\n", f.Recommendation); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  Before: %s\n", f.AutoFix.Before); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  After:  %s\n", f.AutoFix.After); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  Risk:   %s\n", f.AutoFix.Risk); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  Command: %s\n", f.AutoFix.Command); err != nil {
			return err
		}
	}

	if fixableCount > 0 {
		if _, err := fmt.Fprintf(w, "\n%d fixable issue(s) found (dry-run — no changes applied)\n", fixableCount); err != nil {
			return err
		}
	}

	return nil
}

// WriteLintCompact renders a LintResult in one-line-per-finding format
// suitable for CI output.
func WriteLintCompact(w io.Writer, result *LintResult, filePath string) error {
	if result == nil {
		return nil
	}

	for _, f := range result.Findings {
		var sb strings.Builder
		if filePath != "" {
			sb.WriteString(filePath)
			sb.WriteString(": ")
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s", f.Severity, f.Rule, f.Message))
		if _, err := fmt.Fprintln(w, sb.String()); err != nil {
			return err
		}
	}

	return nil
}
