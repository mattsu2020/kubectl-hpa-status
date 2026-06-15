package hpa

import "io"

// WriteIncidentReport writes a comprehensive Markdown incident report suitable
// for incident comments in PagerDuty, Slack, or ticket systems. The report
// includes an incident summary, executive summary table, timeline, root cause
// analysis, affected workloads, remediation steps, capacity context,
// recommendations, and escalation notes.
func WriteIncidentReport(w io.Writer, report StatusReport) error {
	return WriteRichIncidentMarkdown(w, []StatusReport{report})
}
