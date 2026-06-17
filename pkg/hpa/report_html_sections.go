package hpa

import (
	"fmt"
	"strings"
)

// This file holds the per-section HTML renderers extracted from
// WriteHTMLReport (report_html.go) so the orchestrator stays a flat list of
// section calls without a gocyclo exemption.

func writeHTMLOverview(out *strings.Builder, a Analysis) {
	out.WriteString(`<table class="overview">
<tr><th>Target</th><td>`)
	out.WriteString(htmlEscape(a.Target))
	out.WriteString("</td></tr>\n<tr><th>Health</th><td>")
	out.WriteString(htmlHealthBadge(a.Health, a.HealthScore))
	out.WriteString("</td></tr>\n<tr><th>Replicas</th><td>")
	out.WriteString(fmt.Sprintf("current=%d desired=%d min=%d max=%d", a.Current, a.Desired, a.Min, a.Max))
	out.WriteString("</td></tr>\n</table>\n")
}

func writeHTMLSummary(out *strings.Builder, a Analysis) {
	if a.Summary == "" {
		return
	}
	out.WriteString("<h2>Summary</h2>\n<p>")
	out.WriteString(htmlEscape(a.Summary))
	out.WriteString("</p>\n")
}

func writeHTMLConditions(out *strings.Builder, a Analysis) {
	out.WriteString("<h2>Conditions</h2>\n")
	if len(a.Conditions) == 0 {
		out.WriteString("<p><em>No conditions reported.</em></p>\n")
		return
	}
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

func writeHTMLMetrics(out *strings.Builder, a Analysis) {
	out.WriteString("<h2>Metrics</h2>\n")
	if len(a.Metrics) == 0 {
		out.WriteString("<p><em>No current metrics reported.</em></p>\n")
		return
	}
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

func writeHTMLRecommendations(out *strings.Builder, a Analysis) {
	if len(a.Actions) == 0 {
		return
	}
	out.WriteString("<h2>Recommendations</h2>\n<ul>\n")
	for _, action := range a.Actions {
		out.WriteString("<li>")
		out.WriteString(htmlEscape(action))
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n")
}

func writeHTMLSuggestions(out *strings.Builder, a Analysis) {
	if len(a.Suggestions) == 0 {
		return
	}
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

func writeHTMLEvents(out *strings.Builder, report StatusReport) {
	if len(report.Events) == 0 {
		return
	}
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

func writeHTMLPodAnalysis(out *strings.Builder, a Analysis) {
	if a.PodAnalysis == nil {
		return
	}
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

func writeHTMLSimulation(out *strings.Builder, a Analysis) {
	if a.Simulation == nil {
		return
	}
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

func writeHTMLMetricFreshness(out *strings.Builder, a Analysis) {
	if len(a.MetricFreshnessEntries) == 0 {
		return
	}
	out.WriteString("<h2>Metrics Freshness</h2>\n")
	out.WriteString("<table>\n<tr><th>Metric</th><th>Type</th><th>Status</th><th>Source</th><th>Window</th><th>Risk</th></tr>\n")
	for _, mf := range a.MetricFreshnessEntries {
		statusBadge := htmlFreshnessBadge(mf.Status)
		out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			htmlEscape(mf.Name), htmlEscape(mf.Type), statusBadge, htmlEscape(mf.Source), htmlEscape(mf.Window), htmlEscape(mf.Risk)))
	}
	out.WriteString("</table>\n")
	for _, mf := range a.MetricFreshnessEntries {
		if len(mf.Evidence) == 0 && len(mf.NextSteps) == 0 {
			continue
		}
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

func writeHTMLCapacityContext(out *strings.Builder, a Analysis) {
	if a.CapacityContext == nil {
		return
	}
	cc := a.CapacityContext
	if len(cc.PendingPods) == 0 && len(cc.QuotaConstraints) == 0 && len(cc.PDBInterference) == 0 && len(cc.NodeHints) == 0 {
		return
	}
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
