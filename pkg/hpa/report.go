package hpa

import (
	"fmt"
	"strings"
)

// This file holds the shared HTML/Markdown escape and formatting helpers used
// by report_markdown.go, report_html.go, report_list.go, report_incident.go,
// and other renderers across the package (timeline, retrospective, etc.).

// escapeMarkdown escapes pipe characters in table cell content.
func escapeMarkdown(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// htmlEscape escapes special characters for safe HTML content, including
// single quotes so the output is safe in both single- and double-quoted
// attributes.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// htmlHealthBadge returns a color-coded health indicator for HTML.
func htmlHealthBadge(health string, score int) string {
	class := "health-ok"
	switch health {
	case "ERROR":
		class = "health-error"
	case "LIMITED":
		class = "health-limited"
	case "STABILIZED":
		class = "health-stabilized"
	}
	return fmt.Sprintf(`<span class="%s">%s (%d/100)</span>`, class, htmlEscape(health), score)
}

// htmlCSS returns inline CSS for standalone HTML report rendering.
func htmlCSS() string {
	return `body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 2rem auto; max-width: 960px; color: #1a1a1a; background: #fff; }
h1 { border-bottom: 2px solid #e0e0e0; padding-bottom: 0.5rem; }
h2 { margin-top: 1.5rem; color: #333; }
.namespace { font-size: 0.8em; color: #666; }
table { border-collapse: collapse; width: 100%; margin: 0.5rem 0 1rem; }
th, td { border: 1px solid #ddd; padding: 6px 10px; text-align: left; }
th { background: #f5f5f5; font-weight: 600; }
tr:nth-child(even) { background: #fafafa; }
.health-ok { color: #16a34a; font-weight: bold; }
.health-error { color: #dc2626; font-weight: bold; }
.health-limited { color: #d97706; font-weight: bold; }
.health-stabilized { color: #2563eb; font-weight: bold; }
.cond-true { color: #16a34a; }
.cond-false { color: #dc2626; }
.cond-unknown { color: #9ca3af; }
.risk { color: #d97706; font-size: 0.9em; }
pre { background: #f5f5f5; padding: 0.5rem; border-radius: 4px; overflow-x: auto; }
code { font-family: "SF Mono", "Fira Code", monospace; font-size: 0.9em; }
footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #e0e0e0; font-size: 0.85em; color: #888; }
`
}
