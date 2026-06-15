package hpa

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// WriteGitOpsConflictText writes a GitOps conflict report in plain text.
func WriteGitOpsConflictText(w io.Writer, report *GitOpsConflict) error {
	if report == nil {
		_, err := fmt.Fprintln(w, "GitOps Conflict: No data available")
		return err
	}

	var out []byte
	AppendGitOpsConflictText(&out, report, style.Theme{})
	_, err := w.Write(out)
	return err
}

// AppendGitOpsConflictText appends a GitOps conflict section to a byte slice.
// This is used by the main text.go renderer.
func AppendGitOpsConflictText(out *[]byte, report *GitOpsConflict, theme style.Theme) {
	if report == nil {
		*out = append(*out, "\nGitOps Conflict:\n  No GitOps conflict data available.\n"...)
		return
	}

	*out = append(*out, '\n')
	*out = append(*out, "GitOps Conflict: "...)
	*out = append(*out, report.Namespace...)
	*out = append(*out, "/"...)
	*out = append(*out, report.Name...)
	*out = append(*out, " ("...)
	*out = append(*out, report.Target...)
	*out = append(*out, ") \n"...)
	*out = append(*out, theme.SummaryColor(report.Summary)...)
	*out = append(*out, '\n')

	if len(report.Conflicts) == 0 && len(report.Warnings) == 0 {
		*out = append(*out, "  No conflicts or warnings detected.\n"...)
		return
	}

	// Render conflicts
	for _, c := range report.Conflicts {
		appendGitOpsConflictEntry(out, c, theme)
	}

	// Render warnings
	if len(report.Warnings) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, theme.Warning.Render("Warnings:")...)
		*out = append(*out, '\n')
		for _, warn := range report.Warnings {
			*out = append(*out, "  - "...)
			*out = append(*out, warn...)
			*out = append(*out, '\n')
		}
	}

	// Render patches
	if len(report.Patches) > 0 {
		*out = append(*out, '\n')
		*out = append(*out, "Suggested manifest patches:\n"...)
		for _, p := range report.Patches {
			*out = append(*out, "  "...)
			*out = append(*out, p...)
			*out = append(*out, '\n')
		}
	}
}

func appendGitOpsConflictEntry(out *[]byte, c GitOpsConflictEntry, theme style.Theme) {
	*out = append(*out, '\n')
	switch c.Severity {
	case "conflict":
		*out = append(*out, theme.Error.Render("[CONFLICT]")...)
	case "warning":
		*out = append(*out, theme.Warning.Render("[WARNING]")...)
	default:
		*out = append(*out, theme.Dim.Render("[INFO]")...)
	}
	*out = append(*out, " "...)
	*out = append(*out, c.Kind...)
	*out = append(*out, "/"...)
	*out = append(*out, c.Name...)
	if c.Field != "" {
		*out = append(*out, ": "...)
		*out = append(*out, c.Field...)
		*out = append(*out, " conflict\n"...)
	} else {
		*out = append(*out, '\n')
	}

	// Manifest value
	if c.ManifestValue != "" {
		*out = append(*out, "  Manifest: "...)
		*out = append(*out, c.Field...)
		*out = append(*out, "="...)
		*out = append(*out, c.ManifestValue...)
		*out = append(*out, '\n')
	}

	// HPA desired
	if c.HPADesired != "" {
		*out = append(*out, "  HPA desired: "...)
		*out = append(*out, c.Field...)
		*out = append(*out, "="...)
		*out = append(*out, c.HPADesired...)
		*out = append(*out, '\n')
	}

	// Live value
	if c.LiveValue != "" {
		*out = append(*out, "  Live: "...)
		*out = append(*out, c.Field...)
		*out = append(*out, "="...)
		*out = append(*out, c.LiveValue...)
		*out = append(*out, '\n')
	}

	// Detail
	if c.Detail != "" {
		*out = append(*out, "  Impact: "...)
		*out = append(*out, c.Detail...)
		*out = append(*out, '\n')
	}

	// Remediation
	if c.Remediation != "" {
		*out = append(*out, "  Remediation: "...)
		*out = append(*out, theme.ActionLine(c.Remediation)...)
		*out = append(*out, '\n')
	}
}

// WriteGitOpsConflictMarkdown writes a GitOps conflict report in Markdown format.
func WriteGitOpsConflictMarkdown(w io.Writer, report *GitOpsConflict) error {
	if report == nil {
		_, err := fmt.Fprintln(w, "## GitOps Conflict\n\nNo GitOps conflict data available.")
		return err
	}

	_, err := fmt.Fprintf(w, "## GitOps Conflict: %s/%s (%s)\n\n", report.Namespace, report.Name, report.Target)
	if err != nil {
		return err
	}

	if _, err = fmt.Fprintf(w, "**Summary:** %s\n\n", report.Summary); err != nil {
		return err
	}

	if len(report.Conflicts) == 0 && len(report.Warnings) == 0 {
		_, err = fmt.Fprintln(w, "No conflicts or warnings detected.")
		return err
	}

	if err = writeGitOpsMarkdownConflicts(w, report.Conflicts); err != nil {
		return err
	}
	if err = writeGitOpsMarkdownWarnings(w, report.Warnings); err != nil {
		return err
	}
	return writeGitOpsMarkdownPatches(w, report.Patches)
}

func writeGitOpsMarkdownConflicts(w io.Writer, conflicts []GitOpsConflictEntry) error {
	if len(conflicts) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "### Conflicts"); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	if _, err := fmt.Fprintln(w, "| Severity | Kind/Name | Field | Manifest | Live | HPA Desired | Impact | Remediation |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "|----------|-----------|-------|----------|------|-------------|--------|-------------|"); err != nil {
		return err
	}
	for _, c := range conflicts {
		severity := c.Severity
		if severity == "conflict" {
			severity = fmt.Sprintf("**%s**", severity)
		}
		kindName := fmt.Sprintf("%s/%s", c.Kind, c.Name)
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			severity, kindName, c.Field, c.ManifestValue, c.LiveValue, c.HPADesired, c.Detail, c.Remediation); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeGitOpsMarkdownWarnings(w io.Writer, warnings []string) error {
	if len(warnings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "### Warnings"); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	for _, warnMsg := range warnings {
		if _, err := fmt.Fprintf(w, "- %s\n", warnMsg); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeGitOpsMarkdownPatches(w io.Writer, patches []string) error {
	if len(patches) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "### Suggested Patches"); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	if _, err := fmt.Fprintln(w, "```yaml"); err != nil {
		return err
	}
	for _, p := range patches {
		if _, err := fmt.Fprintln(w, p); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, "```")
	return err
}

// WriteGitOpsConflictHTML writes a GitOps conflict report in HTML format.
func WriteGitOpsConflictHTML(w io.Writer, report *GitOpsConflict) error {
	if report == nil {
		_, err := fmt.Fprintln(w, `<h2>GitOps Conflict</h2><p>No GitOps conflict data available.</p>`)
		return err
	}

	_, err := fmt.Fprintf(w, `<h2>GitOps Conflict: %s/%s (%s)</h2>`, report.Namespace, report.Name, report.Target)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, `<p><strong>Summary:</strong> %s</p>`, escapeHTML(report.Summary))
	if err != nil {
		return err
	}

	if len(report.Conflicts) == 0 && len(report.Warnings) == 0 {
		_, err = fmt.Fprintln(w, `<p>No conflicts or warnings detected.</p>`)
		return err
	}

	if err = writeGitOpsHTMLConflicts(w, report.Conflicts); err != nil {
		return err
	}
	if err = writeGitOpsHTMLWarnings(w, report.Warnings); err != nil {
		return err
	}
	return writeGitOpsHTMLPatches(w, report.Patches)
}

func writeGitOpsHTMLConflicts(w io.Writer, conflicts []GitOpsConflictEntry) error {
	if len(conflicts) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, `<h3>Conflicts</h3>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `<table class="conflict-table">`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `<thead><tr><th>Severity</th><th>Kind/Name</th><th>Field</th><th>Manifest</th><th>Live</th><th>HPA Desired</th><th>Impact</th><th>Remediation</th></tr></thead><tbody>`); err != nil {
		return err
	}
	for _, c := range conflicts {
		if _, err := fmt.Fprintf(w, `<tr class="%s"><td>%s</td><td>%s/%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			gitOpsSeverityClass(c.Severity), c.Severity, c.Kind, c.Name, c.Field,
			escapeHTML(c.ManifestValue), escapeHTML(c.LiveValue), escapeHTML(c.HPADesired),
			escapeHTML(c.Detail), escapeHTML(c.Remediation)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, `</tbody></table>`)
	return err
}

func writeGitOpsHTMLWarnings(w io.Writer, warnings []string) error {
	if len(warnings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, `<h3>Warnings</h3><ul>`); err != nil {
		return err
	}
	for _, warnMsg := range warnings {
		if _, err := fmt.Fprintf(w, `<li>%s</li>`, escapeHTML(warnMsg)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, `</ul>`)
	return err
}

func writeGitOpsHTMLPatches(w io.Writer, patches []string) error {
	if len(patches) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, `<h3>Suggested Patches</h3><pre><code>`); err != nil {
		return err
	}
	for _, p := range patches {
		if _, err := fmt.Fprintf(w, "%s\n", escapeHTML(p)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, `</code></pre>`)
	return err
}

// escapeHTML escapes HTML special characters.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// gitOpsSeverityClass maps a GitOps conflict severity to its CSS class.
func gitOpsSeverityClass(s string) string {
	switch s {
	case "conflict":
		return "conflict-error"
	case "warning":
		return "conflict-warning"
	default:
		return "conflict-info"
	}
}
