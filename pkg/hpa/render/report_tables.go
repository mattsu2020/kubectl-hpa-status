package render

import (
	"fmt"
	"strings"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// reportTable is a format-neutral table section. Building the section content
// once and rendering it per format keeps the Markdown and HTML report writers
// from drifting apart on which columns and rows they show.
type reportTable struct {
	title   string
	headers []string
	rows    [][]string
}

// capacityContextTables converts the capacity context into format-neutral
// table sections. NodeHints stay a plain list and are rendered separately.
func capacityContextTables(cc *hpa.CapacityContext) []reportTable {
	var tables []reportTable
	if len(cc.PendingPods) > 0 {
		t := reportTable{title: "Pending Pods", headers: []string{"Name", "Unschedulable", "Reasons"}}
		for _, p := range cc.PendingPods {
			t.rows = append(t.rows, []string{p.Name, fmt.Sprintf("%v", p.Unschedulable), strings.Join(p.Reasons, "; ")})
		}
		tables = append(tables, t)
	}
	if len(cc.QuotaConstraints) > 0 {
		t := reportTable{title: "ResourceQuotas", headers: []string{"Name", "Resource", "Used", "Hard", "Message"}}
		for _, q := range cc.QuotaConstraints {
			t.rows = append(t.rows, []string{q.Name, q.Resource, q.Used, q.Hard, q.Message})
		}
		tables = append(tables, t)
	}
	if len(cc.PDBInterference) > 0 {
		t := reportTable{title: "PodDisruptionBudgets", headers: []string{"Name", "Disruption"}}
		for _, p := range cc.PDBInterference {
			t.rows = append(t.rows, []string{p.Name, p.Disruption})
		}
		tables = append(tables, t)
	}
	return tables
}

func writeMarkdownTable(out *strings.Builder, t reportTable) {
	out.WriteString("### " + t.title + "\n\n")
	out.WriteString("| " + strings.Join(t.headers, " | ") + " |\n")
	out.WriteString(markdownSeparatorRow(t.headers))
	for _, row := range t.rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = rendutil.EscapeMarkdown(cell)
		}
		out.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	out.WriteString("\n")
}

// markdownSeparatorRow pads each column separator to the header cell width
// (header plus its two surrounding spaces), except the last column which the
// historical hand-written tables padded one dash short. Reproducing that
// keeps report output byte-identical across the refactor.
func markdownSeparatorRow(headers []string) string {
	var out strings.Builder
	out.WriteString("|")
	for i, h := range headers {
		width := len(h) + 2
		if i == len(headers)-1 {
			width = len(h) + 1
		}
		out.WriteString(strings.Repeat("-", width))
		out.WriteString("|")
	}
	out.WriteString("\n")
	return out.String()
}

func writeHTMLTable(out *strings.Builder, t reportTable) {
	out.WriteString("<h3>" + rendutil.HTMLEscape(t.title) + "</h3>\n<table>\n<tr>")
	for _, h := range t.headers {
		out.WriteString("<th>" + rendutil.HTMLEscape(h) + "</th>")
	}
	out.WriteString("</tr>\n")
	for _, row := range t.rows {
		out.WriteString("<tr>")
		for _, cell := range row {
			out.WriteString("<td>" + rendutil.HTMLEscape(cell) + "</td>")
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</table>\n")
}
