package bundle

import (
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds the bundle Markdown section renderers for events and the
// metrics diagnostics family (pipeline diagnostics, freshness, contract).
// They were split out of bundle_markdown.go so the top-level orchestrator
// stays a short ordered list of section calls and each metrics sub-table can
// be edited in isolation.

func writeBundleEventsSection(b *Writer, data *Data) {
	b.Print("## Events\n\n")
	if len(data.Events) == 0 {
		b.Print("_No events collected._\n\n---\n\n")
		return
	}
	b.Write(data.Events)
	b.Print("\n---\n\n")
}

func writeBundleMetricsAPI(b *Writer, data *Data) {
	b.Print("## Metrics API Status\n\n")
	b.Write(data.MetricsAPI)
	b.Print("\n\n---\n\n")
}

func writeBundleMetricsDiagnostics(b *Writer, data *Data) {
	b.Print("## Metrics Diagnostics\n\n")
	a := data.StatusReport.Analysis

	hasContent := false
	if a.MetricsDiagnostics != nil {
		writeBundleMetricsDiagnosticsSection(b, a.MetricsDiagnostics)
		hasContent = true
	}
	if len(a.MetricFreshnessEntries) > 0 {
		writeBundleMetricFreshness(b, a.MetricFreshnessEntries)
		hasContent = true
	}
	if a.MetricContract != nil {
		writeBundleMetricContract(b, a.MetricContract)
		hasContent = true
	}

	if !hasContent {
		b.Print("_No metrics diagnostics available._\n")
	}

	b.Print("\n---\n\n")
}

func writeBundleMetricsDiagnosticsSection(b *Writer, md *hpaanalysis.MetricsPipelineDiagnostics) {
	b.Printf("**Overall Status:** %s\n\n", mdEscape(md.OverallStatus))

	if len(md.PerMetricChecks) > 0 {
		b.Print("| Metric Type | Metric Name | Status | Details |\n")
		b.Print("|-------------|-------------|--------|--------|\n")
		for _, check := range md.PerMetricChecks {
			b.Printf("| %s | %s | %s | %s |\n",
				mdEscape(check.MetricType), mdEscape(check.MetricName),
				mdEscape(check.Status), mdEscape(check.Details))
		}
		b.Println()
	}

	if len(md.RemediationSteps) > 0 {
		b.Print("**Remediation Steps:**\n")
		for _, step := range md.RemediationSteps {
			b.Printf("- %s\n", mdEscape(step))
		}
		b.Println()
	}
}

func writeBundleMetricFreshness(b *Writer, entries []hpaanalysis.MetricFreshness) {
	b.Print("### Metric Freshness\n\n")
	b.Print("| Metric | Type | Status | Age | Source |\n")
	b.Print("|--------|------|--------|-----|--------|\n")
	for _, entry := range entries {
		age := "-"
		if entry.Age > 0 {
			age = entry.Age.String()
		}
		source := "-"
		if entry.Source != "" {
			source = mdEscape(entry.Source)
		}
		b.Printf("| %s | %s | %s | %s | %s |\n",
			mdEscape(entry.Name), mdEscape(entry.Type),
			mdEscape(entry.Status), age, source)
	}
	b.Println()
}

func writeBundleMetricContract(b *Writer, contract *hpaanalysis.MetricContractReport) {
	b.Print("### Metric Contract\n\n")
	b.Printf("**Status:** %s\n\n", mdEscape(contract.OverallStatus))
	if len(contract.Checks) == 0 {
		return
	}
	b.Print("| Metric Type | Metric Name | API Service | Available | Data Available | Status |\n")
	b.Print("|-------------|-------------|-------------|-----------|----------------|--------|\n")
	for _, check := range contract.Checks {
		apiAvail := "No"
		if check.APIServiceAvailable {
			apiAvail = "Yes"
		}
		dataAvail := "No"
		if check.DataAvailable {
			dataAvail = "Yes"
		}
		b.Printf("| %s | %s | %s | %s | %s | %s |\n",
			mdEscape(check.MetricType), mdEscape(check.MetricName),
			mdEscape(check.APIService), apiAvail, dataAvail, mdEscape(check.Status))
	}
	b.Println()
}
