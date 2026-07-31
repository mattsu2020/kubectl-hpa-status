package hpa

import (
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// WriteMarkdownListReport writes a ListReport as a Markdown table.
//
// Deprecated: Use render.WriteMarkdownListReport. Kept for API compatibility
// until the v3 facade-removal criteria are met.
func WriteMarkdownListReport(w io.Writer, report ListReport) error {
	return rendutil.WriteMarkdownList(w, listReportViews(report))
}

// WriteHTMLListReport writes a ListReport as a standalone HTML document.
//
// Deprecated: Use render.WriteHTMLListReport. Kept for API compatibility until
// the v3 facade-removal criteria are met.
func WriteHTMLListReport(w io.Writer, report ListReport) error {
	return rendutil.WriteHTMLList(w, listReportViews(report))
}

func listReportViews(report ListReport) []rendutil.ListItemView {
	items := make([]rendutil.ListItemView, len(report.Items))
	for i, item := range report.Items {
		items[i] = rendutil.ListItemView{
			Namespace:   item.Namespace,
			Name:        item.Name,
			Target:      item.Target,
			Current:     item.Current,
			Desired:     item.Desired,
			Min:         item.Min,
			Max:         item.Max,
			Health:      item.Health,
			HealthScore: item.HealthScore,
			Summary:     item.Summary,
		}
	}
	return items
}
