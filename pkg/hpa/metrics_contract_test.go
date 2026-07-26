package hpa

import "testing"

func TestAnalyzeMetricContractReportsUnresolvedObjectResource(t *testing.T) {
	t.Parallel()

	report := AnalyzeMetricContract(MetricContractInput{
		Namespace: "prod",
		HPAName:   "web",
		Metrics: []MetricContractMetric{{
			Type:                    MetricTypeObject,
			Name:                    "queue_depth",
			APIGroup:                "custom.metrics.k8s.io/v1beta1",
			HasCurrentData:          true,
			ResourceResolutionError: "describedObject kind \"Mouse\" was not found",
		}},
		APIServices: map[string]APIServiceStatus{
			"custom.metrics.k8s.io/v1beta1": {Available: true},
		},
	})

	if report.OverallStatus != "degraded" {
		t.Fatalf("OverallStatus = %q, want degraded", report.OverallStatus)
	}
	if len(report.Checks) != 1 || report.Checks[0].Status != "unresolved-resource" {
		t.Fatalf("Checks = %+v, want one unresolved-resource check", report.Checks)
	}
	if commands := GenerateContractCommands(report); len(commands) != 0 {
		t.Fatalf("GenerateContractCommands() = %v, want no guessed command", commands)
	}
}
