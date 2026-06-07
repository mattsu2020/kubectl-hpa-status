package hpa

import (
	"fmt"
)

// MetricContractReport holds the result of metrics contract validation.
type MetricContractReport struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Checks lists the per-metric contract checks.
	Checks []MetricContractCheck `json:"checks" yaml:"checks"`
	// OverallStatus is the aggregated status: "healthy", "degraded", "broken".
	OverallStatus string `json:"overallStatus" yaml:"overallStatus"`
	// Remediation lists recommended actions for fixing issues.
	Remediation []string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
	// Summary is a one-line summary of the contract check.
	Summary string `json:"summary" yaml:"summary"`
}

// MetricContractCheck holds the contract check result for a single metric.
type MetricContractCheck struct {
	// MetricType is the HPA metric type (Resource, Pods, Object, External, ContainerResource).
	MetricType string `json:"metricType" yaml:"metricType"`
	// MetricName is the metric name (e.g., "cpu", "http_requests").
	MetricName string `json:"metricName" yaml:"metricName"`
	// Selector is the label selector for Pods/Object metrics (if present).
	Selector string `json:"selector,omitempty" yaml:"selector,omitempty"`
	// APIService is the metrics API that should serve this metric.
	APIService string `json:"apiService" yaml:"apiService"`
	// APIServiceAvailable indicates whether the APIService was discoverable.
	APIServiceAvailable bool `json:"apiServiceAvailable" yaml:"apiServiceAvailable"`
	// APIServiceMessage explains the APIService availability status.
	APIServiceMessage string `json:"apiServiceMessage,omitempty" yaml:"apiServiceMessage,omitempty"`
	// DataAvailable indicates whether current metric data exists in HPA status.
	DataAvailable bool `json:"dataAvailable" yaml:"dataAvailable"`
	// DataMessage explains the data availability status.
	DataMessage string `json:"dataMessage,omitempty" yaml:"dataMessage,omitempty"`
	// Status is the check status: "ok", "missing-api", "missing-data", "selector-mismatch".
	Status string `json:"status" yaml:"status"`
	// Detail provides additional context about the check result.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	// Remediation suggests a specific action for this metric.
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// MetricContractInput aggregates the data needed for metrics contract analysis.
type MetricContractInput struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// Metrics lists the HPA metric specs to validate.
	Metrics []MetricContractMetric
	// APIServices maps API group/version to availability status.
	APIServices map[string]APIServiceStatus
}

// MetricContractMetric describes a single HPA metric spec for contract validation.
type MetricContractMetric struct {
	// Type is the metric type (Resource, Pods, Object, External, ContainerResource).
	Type string
	// Name is the metric name (e.g., "cpu", "memory", "http_requests").
	Name string
	// Selector is the label selector for Pods/Object metrics (if present).
	Selector string
	// APIGroup is the metrics API group that should serve this metric.
	APIGroup string
	// HasCurrentData indicates whether current metric data exists in HPA status.
	HasCurrentData bool
}

// APIServiceStatus holds the availability status of a metrics API.
type APIServiceStatus struct {
	// Available indicates whether the API is discoverable.
	Available bool
	// Message explains the availability status.
	Message string
}

// AnalyzeMetricContract performs contract validation for HPA metrics.
func AnalyzeMetricContract(input MetricContractInput) *MetricContractReport {
	if len(input.Metrics) == 0 {
		return &MetricContractReport{
			Namespace:     input.Namespace,
			Name:          input.HPAName,
			Target:        input.Target,
			Checks:        []MetricContractCheck{},
			OverallStatus: "healthy",
			Summary:       "No metrics configured; contract is trivially satisfied",
		}
	}

	report := &MetricContractReport{
		Namespace: input.Namespace,
		Name:      input.HPAName,
		Target:    input.Target,
		Checks:    make([]MetricContractCheck, 0, len(input.Metrics)),
	}

	anyMissingAPI := false
	anyMissingData := false

	for _, metric := range input.Metrics {
		check := analyzeMetric(metric, input.APIServices)
		report.Checks = append(report.Checks, check)

		switch check.Status {
		case "missing-api":
			anyMissingAPI = true
		case "missing-data", "selector-mismatch":
			anyMissingData = true
		}
	}

	// Build overall status and remediation
	if anyMissingAPI {
		report.OverallStatus = "broken"
		report.Summary = "Metrics API unavailable; HPA cannot compute desired replicas"
		report.Remediation = append(report.Remediation, "Install and configure the missing metrics adapter or metrics-server")
	} else if anyMissingData {
		report.OverallStatus = "degraded"
		report.Summary = "Metrics APIs are available but not returning current data"
		report.Remediation = append(report.Remediation, "Verify the metric source is healthy and exporting data")
	} else {
		report.OverallStatus = "healthy"
		report.Summary = "All metric references are queryable from metrics APIs"
	}

	// Collect specific remediations from checks
	for _, check := range report.Checks {
		if check.Remediation != "" {
			report.Remediation = append(report.Remediation, check.Remediation)
		}
	}

	return report
}

// analyzeMetric performs contract validation for a single metric.
func analyzeMetric(metric MetricContractMetric, apiServices map[string]APIServiceStatus) MetricContractCheck {
	apiSvc := metricsAPIForMetricType(metric.Type)
	check := MetricContractCheck{
		MetricType: metric.Type,
		MetricName: metric.Name,
		Selector:   metric.Selector,
		APIService: apiSvc,
	}

	// Check APIService availability
	apiStatus, apiExists := apiServices[apiSvc]
	if !apiExists || !apiStatus.Available {
		check.APIServiceAvailable = false
		check.APIServiceMessage = apiStatus.Message
		check.Status = "missing-api"
		check.Detail = fmt.Sprintf("APIService %s is not available", apiSvc)
		check.Remediation = remediationForMissingAPI(apiSvc)
		return check
	}

	check.APIServiceAvailable = true
	check.APIServiceMessage = apiStatus.Message

	// Check data availability
	if !metric.HasCurrentData {
		check.DataAvailable = false
		check.DataMessage = "no current data in HPA status"
		check.Status = "missing-data"
		check.Detail = "Metrics API is available but not returning data"
		check.Remediation = fmt.Sprintf("Verify the %s metric source is healthy and exporting data", metric.Name)
		return check
	}

	check.DataAvailable = true
	check.Status = "ok"
	check.Detail = "Metric is available and current"
	return check
}

// metricsAPIForMetricType maps an HPA metric type to its metrics API.
func metricsAPIForMetricType(metricType string) string {
	switch metricType {
	case "Resource", "ContainerResource":
		return "metrics.k8s.io/v1beta1"
	case "Pods":
		return "custom.metrics.k8s.io/v1beta1"
	case "External":
		return "external.metrics.k8s.io/v1beta1"
	case "Object":
		return "custom.metrics.k8s.io/v1beta1"
	default:
		return "unknown"
	}
}

// remediationForMissingAPI returns a remediation message for a missing metrics API.
func remediationForMissingAPI(apiService string) string {
	switch apiService {
	case "metrics.k8s.io/v1beta1":
		return "Install metrics-server: kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml"
	case "custom.metrics.k8s.io/v1beta1":
		return "Install and configure a metrics adapter (e.g., Prometheus Adapter) for custom.metrics.k8s.io"
	case "external.metrics.k8s.io/v1beta1":
		return "Install and configure a metrics adapter (e.g., KEDA) for external.metrics.k8s.io"
	default:
		return fmt.Sprintf("Verify the %s APIService is installed and healthy", apiService)
	}
}
