package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DiagnoseAdapter builds adapter-focused diagnostics from visible HPA metric
// specs, freshness checks, and metric contract results.
func DiagnoseAdapter(hpa *autoscalingv2.HorizontalPodAutoscaler, freshness []MetricFreshness, contract *MetricContractReport) *AdapterDiagnosticsReport {
	report := &AdapterDiagnosticsReport{
		AdapterType:     "none",
		EndpointHealthy: true,
	}
	if hpa == nil {
		report.EndpointHealthy = false
		report.Checks = append(report.Checks, AdapterCheck{Name: "hpa", Status: "error", Details: "HPA is nil"})
		report.Summary = "Adapter diagnostics could not run without an HPA."
		return report
	}

	metricTypes := adapterMetricTypes(hpa)
	report.AdapterType = summarizeAdapterType(metricTypes)
	if report.AdapterType == "none" {
		report.Checks = append(report.Checks, AdapterCheck{Name: "metric-source", Status: "skipped", Details: "HPA uses only resource metrics"})
		report.Summary = "No custom or external metrics adapter is required by this HPA."
		return report
	}

	report.AvailableMetrics = adapterMetricNames(hpa)
	report.QueryProposals = buildMetricQueryProposals(hpa)
	report.Checks = append(report.Checks, AdapterCheck{
		Name:    "metric-reference",
		Status:  "ok",
		Details: fmt.Sprintf("%d custom/external metric reference(s) found", len(report.AvailableMetrics)),
	})

	report.Checks = append(report.Checks, freshnessAdapterChecks(freshness)...)
	report.Checks = append(report.Checks, contractAdapterChecks(contract)...)

	for _, check := range report.Checks {
		if check.Status == "error" {
			report.EndpointHealthy = false
			if strings.Contains(strings.ToLower(check.Details), "auth") || strings.Contains(strings.ToLower(check.Details), "unauthorized") || strings.Contains(strings.ToLower(check.Details), "forbidden") {
				report.AuthenticationErrors = append(report.AuthenticationErrors, check.Details)
			}
		}
	}
	report.Summary = buildAdapterDiagnosticsSummary(report)
	return report
}

func adapterMetricTypes(hpa *autoscalingv2.HorizontalPodAutoscaler) map[autoscalingv2.MetricSourceType]bool {
	types := map[autoscalingv2.MetricSourceType]bool{}
	for _, metric := range hpa.Spec.Metrics {
		switch metric.Type {
		case autoscalingv2.ExternalMetricSourceType, autoscalingv2.PodsMetricSourceType, autoscalingv2.ObjectMetricSourceType:
			types[metric.Type] = true
		}
	}
	return types
}

func summarizeAdapterType(types map[autoscalingv2.MetricSourceType]bool) string {
	hasCustom := types[autoscalingv2.PodsMetricSourceType] || types[autoscalingv2.ObjectMetricSourceType]
	hasExternal := types[autoscalingv2.ExternalMetricSourceType]
	switch {
	case hasCustom && hasExternal:
		return "custom+external"
	case hasExternal:
		return "external"
	case hasCustom:
		return "custom"
	default:
		return "none"
	}
}

func adapterMetricNames(hpa *autoscalingv2.HorizontalPodAutoscaler) []string {
	var names []string
	for _, metric := range hpa.Spec.Metrics {
		switch metric.Type {
		case autoscalingv2.ExternalMetricSourceType:
			if metric.External != nil {
				names = append(names, string(metric.External.Metric.Name))
			}
		case autoscalingv2.PodsMetricSourceType:
			if metric.Pods != nil {
				names = append(names, string(metric.Pods.Metric.Name))
			}
		case autoscalingv2.ObjectMetricSourceType:
			if metric.Object != nil {
				names = append(names, string(metric.Object.Metric.Name))
			}
		}
	}
	return names
}

func buildMetricQueryProposals(hpa *autoscalingv2.HorizontalPodAutoscaler) []MetricQueryProposal {
	var proposals []MetricQueryProposal
	for _, metric := range hpa.Spec.Metrics {
		switch metric.Type {
		case autoscalingv2.ExternalMetricSourceType:
			if metric.External != nil {
				name := string(metric.External.Metric.Name)
				proposals = append(proposals, MetricQueryProposal{
					MetricName:    name,
					ProposedQuery: fmt.Sprintf("kubectl get --raw '/apis/external.metrics.k8s.io/v1beta1/namespaces/%s/%s'", hpa.Namespace, name),
					Adapter:       "external.metrics.k8s.io",
				})
			}
		case autoscalingv2.PodsMetricSourceType:
			if metric.Pods != nil {
				name := string(metric.Pods.Metric.Name)
				proposals = append(proposals, MetricQueryProposal{
					MetricName:    name,
					ProposedQuery: fmt.Sprintf("kubectl get --raw '/apis/custom.metrics.k8s.io/v1beta1/namespaces/%s/pods/*/%s'", hpa.Namespace, name),
					Adapter:       "custom.metrics.k8s.io",
				})
			}
		case autoscalingv2.ObjectMetricSourceType:
			if metric.Object != nil {
				name := string(metric.Object.Metric.Name)
				proposals = append(proposals, MetricQueryProposal{
					MetricName:    name,
					ProposedQuery: fmt.Sprintf("kubectl get --raw '/apis/custom.metrics.k8s.io/v1beta1/namespaces/%s/%s/%s/%s'", hpa.Namespace, metric.Object.DescribedObject.Kind, metric.Object.DescribedObject.Name, name),
					Adapter:       "custom.metrics.k8s.io",
				})
			}
		}
	}
	return proposals
}

func freshnessAdapterChecks(freshness []MetricFreshness) []AdapterCheck {
	var checks []AdapterCheck
	for _, item := range freshness {
		if item.Type != string(autoscalingv2.ExternalMetricSourceType) && item.Type != string(autoscalingv2.PodsMetricSourceType) && item.Type != string(autoscalingv2.ObjectMetricSourceType) {
			continue
		}
		status := "ok"
		if item.Status == string(FreshnessMissing) || item.Status == string(FreshnessStale) {
			status = "error"
		}
		checks = append(checks, AdapterCheck{
			Name:        "freshness:" + item.Name,
			Status:      status,
			Details:     fmt.Sprintf("%s metric freshness is %s", item.Type, item.Status),
			Remediation: strings.Join(item.NextSteps, "; "),
		})
	}
	return checks
}

func contractAdapterChecks(contract *MetricContractReport) []AdapterCheck {
	if contract == nil {
		return nil
	}
	var checks []AdapterCheck
	for _, issue := range contract.Checks {
		if issue.MetricType != string(autoscalingv2.ExternalMetricSourceType) && issue.MetricType != string(autoscalingv2.PodsMetricSourceType) && issue.MetricType != string(autoscalingv2.ObjectMetricSourceType) {
			continue
		}
		status := "warning"
		if issue.Status == "missing-api" || issue.Status == "missing-data" || issue.Status == "selector-mismatch" {
			status = "error"
		}
		checks = append(checks, AdapterCheck{
			Name:        "contract:" + issue.MetricName,
			Status:      status,
			Details:     issue.Detail,
			Remediation: issue.Remediation,
		})
	}
	if len(checks) == 0 {
		checks = append(checks, AdapterCheck{Name: "metric-contract", Status: "ok", Details: "No metric contract issues found"})
	}
	return checks
}

func buildAdapterDiagnosticsSummary(report *AdapterDiagnosticsReport) string {
	if report.AdapterType == "none" {
		return "No custom or external metrics adapter is required by this HPA."
	}
	if !report.EndpointHealthy {
		return fmt.Sprintf("%s metrics adapter has visible errors; inspect the proposed raw metrics API queries.", report.AdapterType)
	}
	return fmt.Sprintf("%s metrics adapter signals look healthy from visible HPA data.", report.AdapterType)
}
