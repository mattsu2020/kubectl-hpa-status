package hpa

import (
	"fmt"
	"io"
	"strings"
)

// WriteMarkdownReport writes a single StatusReport as a Markdown document.
// The output is suitable for Slack, Notion, or incident reports.
func WriteMarkdownReport(w io.Writer, report StatusReport) error {
	a := report.Analysis
	var out strings.Builder

	out.WriteString("# HPA Status Report: ")
	out.WriteString(a.Name)
	if a.Namespace != "" {
		out.WriteString(" (")
		out.WriteString(a.Namespace)
		out.WriteString(")")
	}
	out.WriteString("\n\n")

	out.WriteString("**Target:** ")
	out.WriteString(a.Target)
	out.WriteString("\n")

	out.WriteString("**Health:** ")
	out.WriteString(a.Health)
	if a.HealthScore > 0 {
		out.WriteString(fmt.Sprintf(" (%d/100)", a.HealthScore))
	}
	out.WriteString("\n")

	out.WriteString(fmt.Sprintf("**Replicas:** current=%d desired=%d min=%d max=%d\n", a.Current, a.Desired, a.Min, a.Max))
	out.WriteString("\n")

	out.WriteString("## Summary\n\n")
	if a.Summary != "" {
		out.WriteString(a.Summary)
		out.WriteString("\n")
	} else {
		out.WriteString("_No summary available._\n")
	}
	out.WriteString("\n")

	// Conditions table
	out.WriteString("## Conditions\n\n")
	if len(a.Conditions) == 0 {
		out.WriteString("_No conditions reported._\n")
	} else {
		out.WriteString("| Type | Status | Reason | Message |\n")
		out.WriteString("|------|--------|--------|--------|\n")
		for _, c := range a.Conditions {
			out.WriteString("| ")
			out.WriteString(c.Type)
			out.WriteString(" | ")
			out.WriteString(c.Status)
			out.WriteString(" | ")
			out.WriteString(c.Reason)
			out.WriteString(" | ")
			out.WriteString(escapeMarkdown(c.Message))
			out.WriteString(" |\n")
		}
	}
	out.WriteString("\n")

	// Metrics table
	out.WriteString("## Metrics\n\n")
	if len(a.Metrics) == 0 {
		out.WriteString("_No current metrics reported._\n")
	} else {
		out.WriteString("| Metric | Current | Target | Ratio |\n")
		out.WriteString("|--------|---------|--------|-------|\n")
		for _, m := range a.Metrics {
			name := m.Name
			if name == "" {
				name = m.Type
			}
			ratio := ""
			if m.Ratio != nil {
				ratio = fmt.Sprintf("%.3f", *m.Ratio)
			}
			out.WriteString("| ")
			out.WriteString(name)
			out.WriteString(" | ")
			out.WriteString(m.Current)
			out.WriteString(" | ")
			out.WriteString(m.Target)
			out.WriteString(" | ")
			out.WriteString(ratio)
			out.WriteString(" |\n")
		}
	}
	out.WriteString("\n")

	// Recommendations
	if len(a.Actions) > 0 {
		out.WriteString("## Recommendations\n\n")
		for _, action := range a.Actions {
			out.WriteString("- ")
			out.WriteString(action)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}

	// Suggestions
	if len(a.Suggestions) > 0 {
		out.WriteString("## Suggestions\n\n")
		for _, s := range a.Suggestions {
			out.WriteString("- **")
			out.WriteString(s.Title)
			out.WriteString("**: ")
			out.WriteString(s.Description)
			out.WriteString("\n")
			if s.Command != "" {
				out.WriteString("  ```\n  ")
				out.WriteString(s.Command)
				out.WriteString("\n  ```\n")
			}
			if s.Risk != "" {
				out.WriteString("  - Risk: ")
				out.WriteString(s.Risk)
				out.WriteString("\n")
			}
		}
		out.WriteString("\n")
	}

	// Events
	if len(report.Events) > 0 {
		out.WriteString("## Events\n\n")
		out.WriteString("| Reason | Message |\n")
		out.WriteString("|--------|--------|\n")
		for _, e := range report.Events {
			out.WriteString("| ")
			out.WriteString(e.Reason)
			out.WriteString(" | ")
			out.WriteString(escapeMarkdown(e.Message))
			out.WriteString(" |\n")
		}
		out.WriteString("\n")
	}

	// Pod Analysis
	if a.PodAnalysis != nil {
		pa := a.PodAnalysis
		out.WriteString("## Pod Analysis\n\n")
		out.WriteString(fmt.Sprintf("- **Total:** %d  **Ready:** %d  **Unready:** %d  **Pending:** %d  **Terminating:** %d\n", pa.Total, pa.Ready, pa.Unready, pa.Pending, pa.Terminating))
		if len(pa.ResourceIssues) > 0 {
			out.WriteString("\n### Missing Resources\n\n")
			out.WriteString("| Pod | Container | Resource | Category |\n")
			out.WriteString("|-----|-----------|----------|----------|\n")
			for _, issue := range pa.ResourceIssues {
				out.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", issue.Pod, issue.Container, issue.Resource, issue.Category))
			}
			out.WriteString("\n")
		}
		if len(pa.ContainerChecks) > 0 {
			out.WriteString("\n### Container Checks\n\n")
			out.WriteString("| Container | Found | Message |\n")
			out.WriteString("|-----------|-------|--------|\n")
			for _, check := range pa.ContainerChecks {
				msg := check.Message
				if msg == "" {
					msg = "OK"
				}
				out.WriteString(fmt.Sprintf("| %s | %v | %s |\n", check.Container, check.Found, escapeMarkdown(msg)))
			}
			out.WriteString("\n")
		}
	}

	// Simulation
	if a.Simulation != nil {
		sim := a.Simulation
		out.WriteString("## Simulation\n\n")
		out.WriteString(fmt.Sprintf("- **Parameter:** %s\n", sim.Parameter))
		out.WriteString(fmt.Sprintf("- **Original:** %s  **Simulated:** %s\n", sim.OriginalValue, sim.SimulatedValue))
		out.WriteString(fmt.Sprintf("- **Before:** desired=%d health=%s(%d)  **After:** desired=%d health=%s(%d)\n",
			sim.Before.DesiredReplicas, sim.Before.Health, sim.Before.HealthScore,
			sim.After.DesiredReplicas, sim.After.Health, sim.After.HealthScore))
		if sim.RiskAssessment != "" {
			out.WriteString(fmt.Sprintf("- **Risk:** %s\n", sim.RiskAssessment))
		}
		if len(sim.Interpretation) > 0 {
			out.WriteString("\n")
			for _, line := range sim.Interpretation {
				out.WriteString(fmt.Sprintf("- %s\n", line))
			}
		}
		out.WriteString("\n")
	}

	// Metrics Freshness
	if len(a.MetricFreshnessEntries) > 0 {
		out.WriteString("## Metrics Freshness\n\n")
		for _, mf := range a.MetricFreshnessEntries {
			out.WriteString(fmt.Sprintf("### %s (%s) — %s\n\n", mf.Name, mf.Type, mf.Status))
			if mf.Source != "" {
				out.WriteString(fmt.Sprintf("- **Source:** %s\n", mf.Source))
			}
			if mf.Window != "" {
				out.WriteString(fmt.Sprintf("- **Window:** %s\n", mf.Window))
			}
			if mf.Risk != "" {
				out.WriteString(fmt.Sprintf("- **Risk:** %s\n", mf.Risk))
			}
			if len(mf.Evidence) > 0 {
				out.WriteString("- **Evidence:**\n")
				for _, e := range mf.Evidence {
					out.WriteString(fmt.Sprintf("  - %s\n", e))
				}
			}
			if len(mf.NextSteps) > 0 {
				out.WriteString("- **Next Steps:**\n")
				for _, ns := range mf.NextSteps {
					out.WriteString(fmt.Sprintf("  - `%s`\n", ns))
				}
			}
			out.WriteString("\n")
		}
	}

	// Capacity Context
	if a.CapacityContext != nil {
		cc := a.CapacityContext
		if len(cc.PendingPods) > 0 || len(cc.QuotaConstraints) > 0 || len(cc.PDBInterference) > 0 || len(cc.NodeHints) > 0 {
			out.WriteString("## Capacity Context\n\n")
			if len(cc.PendingPods) > 0 {
				out.WriteString("### Pending Pods\n\n")
				out.WriteString("| Name | Unschedulable | Reasons |\n")
				out.WriteString("|------|---------------|--------|\n")
				for _, p := range cc.PendingPods {
					reasons := strings.Join(p.Reasons, "; ")
					out.WriteString(fmt.Sprintf("| %s | %v | %s |\n", p.Name, p.Unschedulable, escapeMarkdown(reasons)))
				}
				out.WriteString("\n")
			}
			if len(cc.QuotaConstraints) > 0 {
				out.WriteString("### ResourceQuotas\n\n")
				out.WriteString("| Name | Resource | Used | Hard | Message |\n")
				out.WriteString("|------|----------|------|------|--------|\n")
				for _, q := range cc.QuotaConstraints {
					out.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n", q.Name, q.Resource, q.Used, q.Hard, escapeMarkdown(q.Message)))
				}
				out.WriteString("\n")
			}
			if len(cc.PDBInterference) > 0 {
				out.WriteString("### PodDisruptionBudgets\n\n")
				out.WriteString("| Name | Disruption |\n")
				out.WriteString("|------|-----------|\n")
				for _, p := range cc.PDBInterference {
					out.WriteString(fmt.Sprintf("| %s | %s |\n", p.Name, escapeMarkdown(p.Disruption)))
				}
				out.WriteString("\n")
			}
			if len(cc.NodeHints) > 0 {
				out.WriteString("### Hints\n\n")
				for _, hint := range cc.NodeHints {
					out.WriteString(fmt.Sprintf("- %s\n", hint))
				}
				out.WriteString("\n")
			}
		}
	}

	out.WriteString("---\nGenerated by kubectl-hpa-status\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// WriteHTMLReport writes a single StatusReport as a standalone HTML document
// with inline CSS for portable viewing.
//
//nolint:gocyclo // Sequential HTML template rendering; each section is independent.
func WriteHTMLReport(w io.Writer, report StatusReport) error {
	a := report.Analysis
	var out strings.Builder

	out.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>HPA Status Report: `)
	out.WriteString(htmlEscape(a.Name))
	out.WriteString("</title>\n<style>\n")
	out.WriteString(htmlCSS())
	out.WriteString("</style>\n</head>\n<body>\n")

	out.WriteString(`<h1>HPA Status Report: `)
	out.WriteString(htmlEscape(a.Name))
	if a.Namespace != "" {
		out.WriteString(" <span class=\"namespace\">(")
		out.WriteString(htmlEscape(a.Namespace))
		out.WriteString(")</span>")
	}
	out.WriteString("</h1>\n")

	// Overview table
	out.WriteString(`<table class="overview">
<tr><th>Target</th><td>`)
	out.WriteString(htmlEscape(a.Target))
	out.WriteString("</td></tr>\n<tr><th>Health</th><td>")
	out.WriteString(htmlHealthBadge(a.Health, a.HealthScore))
	out.WriteString("</td></tr>\n<tr><th>Replicas</th><td>")
	out.WriteString(fmt.Sprintf("current=%d desired=%d min=%d max=%d", a.Current, a.Desired, a.Min, a.Max))
	out.WriteString("</td></tr>\n</table>\n")

	// Summary
	if a.Summary != "" {
		out.WriteString("<h2>Summary</h2>\n<p>")
		out.WriteString(htmlEscape(a.Summary))
		out.WriteString("</p>\n")
	}

	// Conditions
	out.WriteString("<h2>Conditions</h2>\n")
	if len(a.Conditions) == 0 {
		out.WriteString("<p><em>No conditions reported.</em></p>\n")
	} else {
		out.WriteString("<table>\n<tr><th>Type</th><th>Status</th><th>Reason</th><th>Message</th></tr>\n")
		for _, c := range a.Conditions {
			out.WriteString("<tr><td>")
			out.WriteString(htmlEscape(c.Type))
			out.WriteString("</td><td>")
			out.WriteString(htmlConditionStatus(c.Status))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(c.Reason))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(c.Message))
			out.WriteString("</td></tr>\n")
		}
		out.WriteString("</table>\n")
	}

	// Metrics
	out.WriteString("<h2>Metrics</h2>\n")
	if len(a.Metrics) == 0 {
		out.WriteString("<p><em>No current metrics reported.</em></p>\n")
	} else {
		out.WriteString("<table>\n<tr><th>Metric</th><th>Current</th><th>Target</th><th>Ratio</th></tr>\n")
		for _, m := range a.Metrics {
			name := m.Name
			if name == "" {
				name = m.Type
			}
			ratio := ""
			if m.Ratio != nil {
				ratio = fmt.Sprintf("%.3f", *m.Ratio)
			}
			out.WriteString("<tr><td>")
			out.WriteString(htmlEscape(name))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(m.Current))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(m.Target))
			out.WriteString("</td><td>")
			out.WriteString(ratio)
			out.WriteString("</td></tr>\n")
		}
		out.WriteString("</table>\n")
	}

	// Recommendations
	if len(a.Actions) > 0 {
		out.WriteString("<h2>Recommendations</h2>\n<ul>\n")
		for _, action := range a.Actions {
			out.WriteString("<li>")
			out.WriteString(htmlEscape(action))
			out.WriteString("</li>\n")
		}
		out.WriteString("</ul>\n")
	}

	// Suggestions
	if len(a.Suggestions) > 0 {
		out.WriteString("<h2>Suggestions</h2>\n<ul>\n")
		for _, s := range a.Suggestions {
			out.WriteString("<li><strong>")
			out.WriteString(htmlEscape(s.Title))
			out.WriteString("</strong>: ")
			out.WriteString(htmlEscape(s.Description))
			out.WriteString("\n")
			if s.Command != "" {
				out.WriteString("<pre><code>")
				out.WriteString(htmlEscape(s.Command))
				out.WriteString("</code></pre>\n")
			}
			if s.Risk != "" {
				out.WriteString("<span class=\"risk\">Risk: ")
				out.WriteString(htmlEscape(s.Risk))
				out.WriteString("</span>\n")
			}
			out.WriteString("</li>\n")
		}
		out.WriteString("</ul>\n")
	}

	// Events
	if len(report.Events) > 0 {
		out.WriteString("<h2>Events</h2>\n<table>\n<tr><th>Reason</th><th>Message</th></tr>\n")
		for _, e := range report.Events {
			out.WriteString("<tr><td>")
			out.WriteString(htmlEscape(e.Reason))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(e.Message))
			out.WriteString("</td></tr>\n")
		}
		out.WriteString("</table>\n")
	}

	// Pod Analysis
	if a.PodAnalysis != nil {
		pa := a.PodAnalysis
		out.WriteString("<h2>Pod Analysis</h2>\n")
		out.WriteString(fmt.Sprintf("<p>Total: %d  Ready: %d  Unready: %d  Pending: %d  Terminating: %d</p>\n", pa.Total, pa.Ready, pa.Unready, pa.Pending, pa.Terminating))
		if len(pa.ResourceIssues) > 0 {
			out.WriteString("<table>\n<tr><th>Pod</th><th>Container</th><th>Resource</th><th>Category</th></tr>\n")
			for _, issue := range pa.ResourceIssues {
				out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
					htmlEscape(issue.Pod), htmlEscape(issue.Container), htmlEscape(issue.Resource), htmlEscape(issue.Category)))
			}
			out.WriteString("</table>\n")
		}
		if len(pa.ContainerChecks) > 0 {
			out.WriteString("<table>\n<tr><th>Container</th><th>Found</th><th>Message</th></tr>\n")
			for _, check := range pa.ContainerChecks {
				msg := check.Message
				if msg == "" {
					msg = "OK"
				}
				out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%v</td><td>%s</td></tr>\n",
					htmlEscape(check.Container), check.Found, htmlEscape(msg)))
			}
			out.WriteString("</table>\n")
		}
	}

	// Simulation
	if a.Simulation != nil {
		sim := a.Simulation
		out.WriteString("<h2>Simulation</h2>\n")
		out.WriteString(fmt.Sprintf("<p><strong>Parameter:</strong> %s &mdash; Original: %s, Simulated: %s</p>\n", htmlEscape(sim.Parameter), htmlEscape(sim.OriginalValue), htmlEscape(sim.SimulatedValue)))
		out.WriteString("<table class=\"overview\">\n")
		out.WriteString("<tr><th></th><th>Before</th><th>After</th></tr>\n")
		out.WriteString(fmt.Sprintf("<tr><td>Desired Replicas</td><td>%d</td><td>%d</td></tr>\n", sim.Before.DesiredReplicas, sim.After.DesiredReplicas))
		out.WriteString(fmt.Sprintf("<tr><td>Health</td><td>%s (%d)</td><td>%s (%d)</td></tr>\n", htmlEscape(sim.Before.Health), sim.Before.HealthScore, htmlEscape(sim.After.Health), sim.After.HealthScore))
		out.WriteString("</table>\n")
		if sim.RiskAssessment != "" {
			out.WriteString(fmt.Sprintf("<p><span class=\"risk\">Risk: %s</span></p>\n", htmlEscape(sim.RiskAssessment)))
		}
		if len(sim.Interpretation) > 0 {
			out.WriteString("<ul>\n")
			for _, line := range sim.Interpretation {
				out.WriteString(fmt.Sprintf("<li>%s</li>\n", htmlEscape(line)))
			}
			out.WriteString("</ul>\n")
		}
	}

	// Metrics Freshness
	if len(a.MetricFreshnessEntries) > 0 {
		out.WriteString("<h2>Metrics Freshness</h2>\n")
		out.WriteString("<table>\n<tr><th>Metric</th><th>Type</th><th>Status</th><th>Source</th><th>Window</th><th>Risk</th></tr>\n")
		for _, mf := range a.MetricFreshnessEntries {
			statusBadge := htmlFreshnessBadge(mf.Status)
			out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
				htmlEscape(mf.Name), htmlEscape(mf.Type), statusBadge, htmlEscape(mf.Source), htmlEscape(mf.Window), htmlEscape(mf.Risk)))
		}
		out.WriteString("</table>\n")
		for _, mf := range a.MetricFreshnessEntries {
			if len(mf.Evidence) > 0 || len(mf.NextSteps) > 0 {
				out.WriteString(fmt.Sprintf("<h3>%s (%s) Details</h3>\n", htmlEscape(mf.Name), htmlEscape(mf.Type)))
				if len(mf.Evidence) > 0 {
					out.WriteString("<p><strong>Evidence:</strong></p>\n<ul>\n")
					for _, e := range mf.Evidence {
						out.WriteString(fmt.Sprintf("<li>%s</li>\n", htmlEscape(e)))
					}
					out.WriteString("</ul>\n")
				}
				if len(mf.NextSteps) > 0 {
					out.WriteString("<p><strong>Next Steps:</strong></p>\n<ul>\n")
					for _, ns := range mf.NextSteps {
						out.WriteString(fmt.Sprintf("<li><code>%s</code></li>\n", htmlEscape(ns)))
					}
					out.WriteString("</ul>\n")
				}
			}
		}
	}

	// Capacity Context
	if a.CapacityContext != nil {
		cc := a.CapacityContext
		if len(cc.PendingPods) > 0 || len(cc.QuotaConstraints) > 0 || len(cc.PDBInterference) > 0 || len(cc.NodeHints) > 0 {
			out.WriteString("<h2>Capacity Context</h2>\n")
			if len(cc.PendingPods) > 0 {
				out.WriteString("<h3>Pending Pods</h3>\n<table>\n<tr><th>Name</th><th>Unschedulable</th><th>Reasons</th></tr>\n")
				for _, p := range cc.PendingPods {
					reasons := strings.Join(p.Reasons, "; ")
					out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%v</td><td>%s</td></tr>\n",
						htmlEscape(p.Name), p.Unschedulable, htmlEscape(reasons)))
				}
				out.WriteString("</table>\n")
			}
			if len(cc.QuotaConstraints) > 0 {
				out.WriteString("<h3>ResourceQuotas</h3>\n<table>\n<tr><th>Name</th><th>Resource</th><th>Used</th><th>Hard</th><th>Message</th></tr>\n")
				for _, q := range cc.QuotaConstraints {
					out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
						htmlEscape(q.Name), htmlEscape(q.Resource), htmlEscape(q.Used), htmlEscape(q.Hard), htmlEscape(q.Message)))
				}
				out.WriteString("</table>\n")
			}
			if len(cc.PDBInterference) > 0 {
				out.WriteString("<h3>PodDisruptionBudgets</h3>\n<table>\n<tr><th>Name</th><th>Disruption</th></tr>\n")
				for _, p := range cc.PDBInterference {
					out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td></tr>\n",
						htmlEscape(p.Name), htmlEscape(p.Disruption)))
				}
				out.WriteString("</table>\n")
			}
			if len(cc.NodeHints) > 0 {
				out.WriteString("<h3>Hints</h3>\n<ul>\n")
				for _, hint := range cc.NodeHints {
					out.WriteString(fmt.Sprintf("<li>%s</li>\n", htmlEscape(hint)))
				}
				out.WriteString("</ul>\n")
			}
		}
	}

	out.WriteString("<footer>Generated by kubectl-hpa-status</footer>\n")
	out.WriteString("</body>\n</html>\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// WriteMarkdownListReport writes a ListReport as a Markdown table.
func WriteMarkdownListReport(w io.Writer, report ListReport) error {
	var out strings.Builder

	out.WriteString("# HPA List Report\n\n")

	if len(report.Items) == 0 {
		out.WriteString("_No HPAs found._\n")
		_, err := io.WriteString(w, out.String())
		return err
	}

	out.WriteString("| Namespace | Name | Target | Current | Desired | Min | Max | Health | Score | Summary |\n")
	out.WriteString("|-----------|------|--------|---------|---------|-----|-----|--------|-------|--------|\n")

	for _, item := range report.Items {
		out.WriteString("| ")
		out.WriteString(item.Namespace)
		out.WriteString(" | ")
		out.WriteString(item.Name)
		out.WriteString(" | ")
		out.WriteString(item.Target)
		out.WriteString(" | ")
		out.WriteString(fmt.Sprintf("%d", item.Current))
		out.WriteString(" | ")
		out.WriteString(fmt.Sprintf("%d", item.Desired))
		out.WriteString(" | ")
		out.WriteString(fmt.Sprintf("%d", item.Min))
		out.WriteString(" | ")
		out.WriteString(fmt.Sprintf("%d", item.Max))
		out.WriteString(" | ")
		out.WriteString(item.Health)
		out.WriteString(" | ")
		out.WriteString(fmt.Sprintf("%d", item.HealthScore))
		out.WriteString(" | ")
		out.WriteString(escapeMarkdown(item.Summary))
		out.WriteString(" |\n")
	}

	out.WriteString("\n---\nGenerated by kubectl-hpa-status\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// WriteHTMLListReport writes a ListReport as a standalone HTML document.
func WriteHTMLListReport(w io.Writer, report ListReport) error {
	var out strings.Builder

	out.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>HPA List Report</title>
<style>
`)
	out.WriteString(htmlCSS())
	out.WriteString("</style>\n</head>\n<body>\n")
	out.WriteString("<h1>HPA List Report</h1>\n")

	if len(report.Items) == 0 {
		out.WriteString("<p><em>No HPAs found.</em></p>\n")
	} else {
		out.WriteString("<table>\n<tr><th>Namespace</th><th>Name</th><th>Target</th><th>Current</th><th>Desired</th><th>Min</th><th>Max</th><th>Health</th><th>Score</th><th>Summary</th></tr>\n")
		for _, item := range report.Items {
			out.WriteString("<tr><td>")
			out.WriteString(htmlEscape(item.Namespace))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(item.Name))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(item.Target))
			out.WriteString("</td><td>")
			out.WriteString(fmt.Sprintf("%d", item.Current))
			out.WriteString("</td><td>")
			out.WriteString(fmt.Sprintf("%d", item.Desired))
			out.WriteString("</td><td>")
			out.WriteString(fmt.Sprintf("%d", item.Min))
			out.WriteString("</td><td>")
			out.WriteString(fmt.Sprintf("%d", item.Max))
			out.WriteString("</td><td>")
			out.WriteString(htmlHealthBadge(item.Health, item.HealthScore))
			out.WriteString("</td><td>")
			out.WriteString(fmt.Sprintf("%d", item.HealthScore))
			out.WriteString("</td><td>")
			out.WriteString(htmlEscape(item.Summary))
			out.WriteString("</td></tr>\n")
		}
		out.WriteString("</table>\n")
	}

	out.WriteString("<footer>Generated by kubectl-hpa-status</footer>\n")
	out.WriteString("</body>\n</html>\n")

	_, err := io.WriteString(w, out.String())
	return err
}

// escapeMarkdown escapes pipe characters in table cell content.
func escapeMarkdown(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// htmlEscape escapes special characters for safe HTML content.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
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

// htmlConditionStatus returns a color-coded condition status for HTML.
func htmlConditionStatus(status string) string {
	class := "cond-unknown"
	switch status {
	case "True":
		class = "cond-true"
	case "False":
		class = "cond-false"
	}
	return fmt.Sprintf(`<span class="%s">%s</span>`, class, htmlEscape(status))
}

// htmlFreshnessBadge returns a color-coded freshness status badge for HTML.
func htmlFreshnessBadge(status string) string {
	class := "cond-unknown"
	switch status {
	case "OK":
		class = "cond-true"
	case "Missing":
		class = "cond-false"
	case "Stale":
		class = "health-limited"
	}
	return fmt.Sprintf(`<span class="%s">%s</span>`, class, htmlEscape(status))
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
