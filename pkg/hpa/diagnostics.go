package hpa

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DiagnoseMetricsPipeline performs a comprehensive health check of the metrics
// pipeline by comparing spec metrics against current metrics in the HPA status.
// It returns per-metric health checks and remediation steps for any issues found.
func DiagnoseMetricsPipeline(hpa *autoscalingv2.HorizontalPodAutoscaler) *MetricsPipelineDiagnostics {
	if hpa == nil {
		return nil
	}

	specMetrics := hpa.Spec.Metrics
	currentMetrics := hpa.Status.CurrentMetrics

	if len(specMetrics) == 0 {
		return &MetricsPipelineDiagnostics{
			OverallStatus:   "healthy",
			PerMetricChecks: nil,
			RemediationSteps: []string{
				"No spec metrics are configured; the HPA relies on default resource metrics or has no metric source.",
			},
		}
	}

	// When all spec metrics exist but current metrics is empty, the metrics
	// server or custom metrics adapter is likely down.
	allCurrentMissing := len(currentMetrics) == 0

	var checks []PerMetricHealthCheck
	var remediationSteps []string
	hasMissing := false

	for _, spec := range specMetrics {
		metricType, metricName := specMetricIdentity(spec)
		check := PerMetricHealthCheck{
			MetricType: metricType,
			MetricName: metricName,
		}

		if allCurrentMissing {
			check.Status = "missing"
			check.Details = fmt.Sprintf(
				"%s metric %q is configured but no current metrics are reported at all; the metrics server or adapter is likely down.",
				metricType, metricName,
			)
			check.Remediation = "Verify that the metrics server or custom metrics adapter is running and accessible: kubectl get pods -n kube-system | grep metrics; kubectl logs -n kube-system <metrics-pod>."
			hasMissing = true
			checks = append(checks, check)
			continue
		}

		if found := findMatchingCurrentMetric(spec, currentMetrics); found {
			check.Status = "healthy"
			check.Details = fmt.Sprintf("%s metric %q is reporting current values.", metricType, metricName)
		} else {
			check.Status = "missing"
			check.Details = fmt.Sprintf(
				"%s metric %q is configured but no matching current metric status is reported.",
				metricType, metricName,
			)
			check.Remediation = buildMetricRemediation(spec)
			hasMissing = true
		}

		checks = append(checks, check)
	}

	overallStatus := "healthy"
	if allCurrentMissing {
		overallStatus = "error"
		remediationSteps = append(remediationSteps,
			"All spec metrics have no corresponding current metrics. The metrics pipeline is not delivering data to the HPA controller.",
			"Check metrics-server deployment: kubectl get deploy metrics-server -n kube-system.",
			"Verify API service registration: kubectl get apiservice v1beta1.metrics.k8s.io.",
			"If using a custom/external metrics adapter, check its pods and logs.",
			"Ensure NetworkPolicy or firewall rules allow the metrics server to scrape kubelets.",
		)
	} else if hasMissing {
		overallStatus = "degraded"
		remediationSteps = append(remediationSteps,
			"One or more spec metrics are not reporting current values. Check the specific metric adapter and metric availability.",
		)
		for _, check := range checks {
			if check.Status == "missing" && check.Remediation != "" {
				remediationSteps = append(remediationSteps, check.Remediation)
			}
		}
	}

	return &MetricsPipelineDiagnostics{
		OverallStatus:    overallStatus,
		PerMetricChecks:  checks,
		RemediationSteps: remediationSteps,
	}
}
