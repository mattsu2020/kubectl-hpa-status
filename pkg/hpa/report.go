package hpa

import "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"

// This file is a thin facade re-exporting the shared HTML/Markdown helpers
// that now live in pkg/hpa/rendutil. The helpers were extracted to break the
// import cycle that blocks a pkg/hpa/render split: both the remaining *_text.go
// files in this package and the new render package need them, so they live
// below both in the dependency graph. Callers in pkg/hpa keep using the
// unexported names below; new code should import rendutil directly.

func escapeMarkdown(s string) string { return rendutil.EscapeMarkdown(s) }
func htmlEscape(s string) string     { return rendutil.HTMLEscape(s) }
func htmlHealthBadge(health string, score int) string {
	return rendutil.HTMLHealthBadge(health, score)
}
func htmlCSS() string { return rendutil.HTMLCSS() }
