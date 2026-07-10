package render

import (
	"io"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// WriteMarkdownListReport writes a ListReport as a Markdown table.
func WriteMarkdownListReport(w io.Writer, report hpa.ListReport) error {
	return hpa.WriteMarkdownListReport(w, report)
}

// WriteHTMLListReport writes a ListReport as a standalone HTML document.
func WriteHTMLListReport(w io.Writer, report hpa.ListReport) error {
	return hpa.WriteHTMLListReport(w, report)
}
