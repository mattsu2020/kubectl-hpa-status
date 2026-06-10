package hpa

import "fmt"

// AppendAdapterDiagnosticsText appends adapter diagnostics to status text.
func AppendAdapterDiagnosticsText(out *[]byte, report *AdapterDiagnosticsReport) {
	if report == nil {
		return
	}
	*out = fmt.Appendf(*out, "Adapter Diagnostics:\n")
	*out = fmt.Appendf(*out, "  Type: %s\n", report.AdapterType)
	*out = fmt.Appendf(*out, "  Healthy: %t\n", report.EndpointHealthy)
	*out = fmt.Appendf(*out, "  Summary: %s\n", report.Summary)
	if len(report.QueryProposals) > 0 {
		*out = fmt.Appendf(*out, "  Query proposals:\n")
		for _, proposal := range report.QueryProposals {
			*out = fmt.Appendf(*out, "    - %s (%s): %s\n", proposal.MetricName, proposal.Adapter, proposal.ProposedQuery)
		}
	}
	if len(report.Checks) > 0 {
		*out = fmt.Appendf(*out, "  Checks:\n")
		for _, check := range report.Checks {
			*out = fmt.Appendf(*out, "    - %s: %s", check.Name, check.Status)
			if check.Details != "" {
				*out = fmt.Appendf(*out, " - %s", check.Details)
			}
			*out = append(*out, '\n')
			if check.Remediation != "" {
				*out = fmt.Appendf(*out, "      Remediation: %s\n", check.Remediation)
			}
		}
	}
}
