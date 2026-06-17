package cmd

import (
	"fmt"
	"html"
	"io"
	"sort"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func writeClusterSummaryMarkdown(out io.Writer, report hpaanalysis.ListReport) error {
	items := append([]hpaanalysis.ListItem(nil), report.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].HealthScore < items[j].HealthScore
	})
	counts := clusterHealthCounts(items)

	_, _ = fmt.Fprintln(out, "# HPA Cluster Health Report")
	_, _ = fmt.Fprintln(out, "\n## Summary")
	_, _ = fmt.Fprintf(out, "- Total HPAs: %d\n", len(items))
	_, _ = fmt.Fprintf(out, "- Healthy: %d\n", counts[string(hpaanalysis.HealthOK)])
	_, _ = fmt.Fprintf(out, "- Limited: %d\n", counts[string(hpaanalysis.HealthLimited)]+counts[hpaanalysis.ConditionScalingLimited])
	_, _ = fmt.Fprintf(out, "- Error: %d\n", counts[string(hpaanalysis.HealthError)])
	_, _ = fmt.Fprintf(out, "- Stabilized: %d\n", counts[string(hpaanalysis.HealthStabilized)])

	_, _ = fmt.Fprintln(out, "\n## Worst HPAs")
	_, _ = fmt.Fprintln(out, "| Namespace | HPA | Score | Main Issue |")
	_, _ = fmt.Fprintln(out, "|---|---|---:|---|")
	for _, item := range firstNListItems(items, 10) {
		issue := item.Issue
		if issue == "" {
			issue = item.Summary
		}
		_, _ = fmt.Fprintf(out, "| %s | %s | %d | %s |\n",
			escapeMarkdownCell(item.Namespace), escapeMarkdownCell(item.Name), item.HealthScore, escapeMarkdownCell(issue))
	}

	actions := prioritizedListActions(items)
	if len(actions) > 0 {
		_, _ = fmt.Fprintln(out, "\n## Recommended Actions")
		for _, action := range actions {
			_, _ = fmt.Fprintf(out, "- %s\n", action)
		}
	}
	return nil
}

func writeClusterSummaryHTML(out io.Writer, report hpaanalysis.ListReport) error {
	items := append([]hpaanalysis.ListItem(nil), report.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].HealthScore < items[j].HealthScore
	})
	counts := clusterHealthCounts(items)
	_, _ = fmt.Fprintln(out, "<!doctype html><html><head><meta charset=\"utf-8\"><title>HPA Cluster Health Report</title></head><body>")
	_, _ = fmt.Fprintln(out, "<h1>HPA Cluster Health Report</h1>")
	_, _ = fmt.Fprintf(out, "<h2>Summary</h2><ul><li>Total HPAs: %d</li><li>Healthy: %d</li><li>Limited: %d</li><li>Error: %d</li><li>Stabilized: %d</li></ul>",
		len(items), counts[string(hpaanalysis.HealthOK)], counts[string(hpaanalysis.HealthLimited)]+counts[hpaanalysis.ConditionScalingLimited], counts[string(hpaanalysis.HealthError)], counts[string(hpaanalysis.HealthStabilized)])
	_, _ = fmt.Fprintln(out, "<h2>Worst HPAs</h2><table><tr><th>Namespace</th><th>HPA</th><th>Score</th><th>Main Issue</th></tr>")
	for _, item := range firstNListItems(items, 10) {
		issue := item.Issue
		if issue == "" {
			issue = item.Summary
		}
		_, _ = fmt.Fprintf(out, "<tr><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>",
			html.EscapeString(item.Namespace), html.EscapeString(item.Name), item.HealthScore, html.EscapeString(issue))
	}
	_, _ = fmt.Fprintln(out, "</table>")
	if actions := prioritizedListActions(items); len(actions) > 0 {
		_, _ = fmt.Fprintln(out, "<h2>Recommended Actions</h2><ul>")
		for _, action := range actions {
			_, _ = fmt.Fprintf(out, "<li>%s</li>", html.EscapeString(action))
		}
		_, _ = fmt.Fprintln(out, "</ul>")
	}
	_, _ = fmt.Fprintln(out, "</body></html>")
	return nil
}

func clusterHealthCounts(items []hpaanalysis.ListItem) map[string]int {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Health]++
	}
	return counts
}

func firstNListItems(items []hpaanalysis.ListItem, n int) []hpaanalysis.ListItem {
	if len(items) < n {
		return items
	}
	return items[:n]
}

func prioritizedListActions(items []hpaanalysis.ListItem) []string {
	var actions []string
	for _, item := range firstNListItems(items, 5) {
		if item.Health == string(hpaanalysis.HealthOK) && item.Issue == "" {
			continue
		}
		action := fmt.Sprintf("%s/%s: inspect %s", item.Namespace, item.Name, item.Health)
		if item.Issue != "" {
			action += " (" + item.Issue + ")"
		}
		actions = append(actions, action)
	}
	return actions
}

func escapeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	if value == "" {
		return "-"
	}
	return value
}
