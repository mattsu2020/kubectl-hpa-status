package render

import (
	"fmt"
	"strings"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/rendutil"
)

// This file holds the per-section HTML renderers extracted from
// WriteHTMLReport (report_html.go) so the orchestrator stays a flat list of
// section calls without a gocyclo exemption.

func writeHTMLOverview(out *strings.Builder, a hpa.Analysis) {
	out.WriteString(`<table class="overview">
<tr><th>Target</th><td>`)
	out.WriteString(rendutil.HTMLEscape(a.Target))
	out.WriteString("</td></tr>\n<tr><th>Health</th><td>")
	out.WriteString(rendutil.HTMLHealthBadge(a.Health, a.HealthScore))
	out.WriteString("</td></tr>\n<tr><th>Replicas</th><td>")
	out.WriteString(fmt.Sprintf("current=%d desired=%d min=%d max=%d", a.Current, a.Desired, a.Min, a.Max))
	out.WriteString("</td></tr>\n</table>\n")
}

func writeHTMLSummary(out *strings.Builder, a hpa.Analysis) {
	if a.Summary == "" {
		return
	}
	out.WriteString("<h2>Summary</h2>\n<p>")
	out.WriteString(rendutil.HTMLEscape(a.Summary))
	out.WriteString("</p>\n")
}

func writeHTMLConditions(out *strings.Builder, a hpa.Analysis) {
	out.WriteString("<h2>Conditions</h2>\n")
	if len(a.Conditions) == 0 {
		out.WriteString("<p><em>No conditions reported.</em></p>\n")
		return
	}
	out.WriteString("<table>\n<tr><th>Type</th><th>Status</th><th>Reason</th><th>Message</th></tr>\n")
	for _, c := range a.Conditions {
		out.WriteString("<tr><td>")
		out.WriteString(rendutil.HTMLEscape(c.Type))
		out.WriteString("</td><td>")
		out.WriteString(htmlConditionStatus(c.Status))
		out.WriteString("</td><td>")
		out.WriteString(rendutil.HTMLEscape(c.Reason))
		out.WriteString("</td><td>")
		out.WriteString(rendutil.HTMLEscape(c.Message))
		out.WriteString("</td></tr>\n")
	}
	out.WriteString("</table>\n")
}

func writeHTMLMetrics(out *strings.Builder, a hpa.Analysis) {
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
		out.WriteString(rendutil.HTMLEscape(name))
		out.WriteString("</td><td>")
		out.WriteString(rendutil.HTMLEscape(m.Current))
		out.WriteString("</td><td>")
		out.WriteString(rendutil.HTMLEscape(m.Target))
		out.WriteString("</td><td>")
		out.WriteString(ratio)
		out.WriteString("</td></tr>\n")
	}
	out.WriteString("</table>\n")
}

func writeHTMLRecommendations(out *strings.Builder, a hpa.Analysis) {
	if len(a.Actions) == 0 {
		return
	}
	out.WriteString("<h2>Recommendations</h2>\n<ul>\n")
	for _, action := range a.Actions {
		out.WriteString("<li>")
		out.WriteString(rendutil.HTMLEscape(action))
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n")
}

func writeHTMLSuggestions(out *strings.Builder, a hpa.Analysis) {
	if len(a.Suggestions) == 0 {
		return
	}
	out.WriteString("<h2>Suggestions</h2>\n<ul>\n")
	for _, s := range a.Suggestions {
		out.WriteString("<li><strong>")
		out.WriteString(rendutil.HTMLEscape(s.Title))
		out.WriteString("</strong>: ")
		out.WriteString(rendutil.HTMLEscape(s.Description))
		out.WriteString("\n")
		if s.Command != "" {
			out.WriteString("<pre><code>")
			out.WriteString(rendutil.HTMLEscape(s.Command))
			out.WriteString("</code></pre>\n")
		}
		if s.Risk != "" {
			out.WriteString("<span class=\"risk\">Risk: ")
			out.WriteString(rendutil.HTMLEscape(s.Risk))
			out.WriteString("</span>\n")
		}
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n")
}

func writeHTMLEvents(out *strings.Builder, report hpa.StatusReport) {
	if len(report.Events) == 0 {
		return
	}
	out.WriteString("<h2>Events</h2>\n<table>\n<tr><th>Reason</th><th>Message</th></tr>\n")
	for _, e := range report.Events {
		out.WriteString("<tr><td>")
		out.WriteString(rendutil.HTMLEscape(e.Reason))
		out.WriteString("</td><td>")
		out.WriteString(rendutil.HTMLEscape(e.Message))
		out.WriteString("</td></tr>\n")
	}
	out.WriteString("</table>\n")
}

func writeHTMLPodAnalysis(out *strings.Builder, a hpa.Analysis) {
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
				rendutil.HTMLEscape(issue.Pod), rendutil.HTMLEscape(issue.Container), rendutil.HTMLEscape(issue.Resource), rendutil.HTMLEscape(issue.Category)))
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
				rendutil.HTMLEscape(check.Container), check.Found, rendutil.HTMLEscape(msg)))
		}
		out.WriteString("</table>\n")
	}
}

func writeHTMLSimulation(out *strings.Builder, a hpa.Analysis) {
	if a.FlappingSimulation == nil {
		return
	}
	sim := a.FlappingSimulation
	out.WriteString("<h2>Simulation</h2>\n")
	out.WriteString(fmt.Sprintf("<p><strong>Parameter:</strong> %s &mdash; Original: %s, Simulated: %s</p>\n", rendutil.HTMLEscape(sim.Parameter), rendutil.HTMLEscape(sim.OriginalValue), rendutil.HTMLEscape(sim.SimulatedValue)))
	out.WriteString("<table class=\"overview\">\n")
	out.WriteString("<tr><th></th><th>Before</th><th>After</th></tr>\n")
	out.WriteString(fmt.Sprintf("<tr><td>Desired Replicas</td><td>%d</td><td>%d</td></tr>\n", sim.Before.DesiredReplicas, sim.After.DesiredReplicas))
	out.WriteString(fmt.Sprintf("<tr><td>Health</td><td>%s (%d)</td><td>%s (%d)</td></tr>\n", rendutil.HTMLEscape(sim.Before.Health), sim.Before.HealthScore, rendutil.HTMLEscape(sim.After.Health), sim.After.HealthScore))
	out.WriteString("</table>\n")
	if sim.RiskAssessment != "" {
		out.WriteString(fmt.Sprintf("<p><span class=\"risk\">Risk: %s</span></p>\n", rendutil.HTMLEscape(sim.RiskAssessment)))
	}
	if len(sim.Interpretation) > 0 {
		out.WriteString("<ul>\n")
		for _, line := range sim.Interpretation {
			out.WriteString(fmt.Sprintf("<li>%s</li>\n", rendutil.HTMLEscape(line)))
		}
		out.WriteString("</ul>\n")
	}
}

func writeHTMLMetricFreshness(out *strings.Builder, a hpa.Analysis) {
	if len(a.MetricFreshnessEntries) == 0 {
		return
	}
	out.WriteString("<h2>Metrics Freshness</h2>\n")
	out.WriteString("<table>\n<tr><th>Metric</th><th>Type</th><th>Status</th><th>Source</th><th>Window</th><th>Risk</th></tr>\n")
	for _, mf := range a.MetricFreshnessEntries {
		statusBadge := htmlFreshnessBadge(mf.Status)
		out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			rendutil.HTMLEscape(mf.Name), rendutil.HTMLEscape(mf.Type), statusBadge, rendutil.HTMLEscape(mf.Source), rendutil.HTMLEscape(mf.Window), rendutil.HTMLEscape(mf.Risk)))
	}
	out.WriteString("</table>\n")
	for _, mf := range a.MetricFreshnessEntries {
		if len(mf.Evidence) == 0 && len(mf.NextSteps) == 0 {
			continue
		}
		out.WriteString(fmt.Sprintf("<h3>%s (%s) Details</h3>\n", rendutil.HTMLEscape(mf.Name), rendutil.HTMLEscape(mf.Type)))
		if len(mf.Evidence) > 0 {
			out.WriteString("<p><strong>Evidence:</strong></p>\n<ul>\n")
			for _, e := range mf.Evidence {
				out.WriteString(fmt.Sprintf("<li>%s</li>\n", rendutil.HTMLEscape(e)))
			}
			out.WriteString("</ul>\n")
		}
		if len(mf.NextSteps) > 0 {
			out.WriteString("<p><strong>Next Steps:</strong></p>\n<ul>\n")
			for _, ns := range mf.NextSteps {
				out.WriteString(fmt.Sprintf("<li><code>%s</code></li>\n", rendutil.HTMLEscape(ns)))
			}
			out.WriteString("</ul>\n")
		}
	}
}

func writeHTMLCapacityContext(out *strings.Builder, a hpa.Analysis) {
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
				rendutil.HTMLEscape(p.Name), p.Unschedulable, rendutil.HTMLEscape(reasons)))
		}
		out.WriteString("</table>\n")
	}
	if len(cc.QuotaConstraints) > 0 {
		out.WriteString("<h3>ResourceQuotas</h3>\n<table>\n<tr><th>Name</th><th>Resource</th><th>Used</th><th>Hard</th><th>Message</th></tr>\n")
		for _, q := range cc.QuotaConstraints {
			out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
				rendutil.HTMLEscape(q.Name), rendutil.HTMLEscape(q.Resource), rendutil.HTMLEscape(q.Used), rendutil.HTMLEscape(q.Hard), rendutil.HTMLEscape(q.Message)))
		}
		out.WriteString("</table>\n")
	}
	if len(cc.PDBInterference) > 0 {
		out.WriteString("<h3>PodDisruptionBudgets</h3>\n<table>\n<tr><th>Name</th><th>Disruption</th></tr>\n")
		for _, p := range cc.PDBInterference {
			out.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td></tr>\n",
				rendutil.HTMLEscape(p.Name), rendutil.HTMLEscape(p.Disruption)))
		}
		out.WriteString("</table>\n")
	}
	if len(cc.NodeHints) > 0 {
		out.WriteString("<h3>Hints</h3>\n<ul>\n")
		for _, hint := range cc.NodeHints {
			out.WriteString(fmt.Sprintf("<li>%s</li>\n", rendutil.HTMLEscape(hint)))
		}
		out.WriteString("</ul>\n")
	}
}
