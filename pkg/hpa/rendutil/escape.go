// Package rendutil holds the shared HTML/Markdown escape and formatting
// helpers used by renderers in pkg/hpa (report_*.go, *_text.go, timeline,
// retrospective, etc.). Extracting them into their own package breaks the
// import cycle that would otherwise block a pkg/hpa/render split: both
// pkg/hpa (the remaining *_text.go files) and pkg/hpa/render need these
// helpers, so they must live below both in the dependency graph.
package rendutil

import "strings"

// EscapeMarkdown escapes pipe characters in table cell content.
func EscapeMarkdown(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// HTMLEscape escapes special characters for safe HTML content, including
// single quotes so the output is safe in both single- and double-quoted
// attributes.
func HTMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
