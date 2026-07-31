package render

import (
	"io"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// WriteMarkdownListReport writes a ListReport as a Markdown table.
func WriteMarkdownListReport(w io.Writer, report hpa.ListReport) error {
	return rendutil.WriteMarkdownList(w, listItemViews(report))
}

// WriteHTMLListReport writes a ListReport as a standalone HTML document.
func WriteHTMLListReport(w io.Writer, report hpa.ListReport) error {
	return rendutil.WriteHTMLList(w, listItemViews(report))
}

func listItemViews(report hpa.ListReport) []rendutil.ListItemView {
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
