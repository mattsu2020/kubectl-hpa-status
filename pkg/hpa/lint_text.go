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

	if _, err := fmt.Fprintf(w, "\n%d error(s), %d warning(s), %d info(s)\n",
		result.Errors, result.Warnings, result.Infos); err != nil {
		return err
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
