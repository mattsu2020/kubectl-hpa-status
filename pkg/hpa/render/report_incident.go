package render

import (
	"io"

	hpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// WriteIncidentReport writes a comprehensive Markdown incident report suitable
// for incident comments in PagerDuty, Slack, or ticket systems. The report
// body is assembled by hpa.WriteRichIncidentMarkdown, which stays in pkg/hpa
// because it depends on many pkg/hpa analysis types directly.
func WriteIncidentReport(w io.Writer, report hpa.StatusReport) error {
	return hpa.WriteRichIncidentMarkdown(w, []hpa.StatusReport{report})
}
