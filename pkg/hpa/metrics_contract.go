package hpa

import (
	"fmt"
	"strings"
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
	// identity retains the canonical spec identity for internal correlation
	// without changing the public JSON/YAML contract report.
	identity MetricID
	// MetricType is the HPA metric type (Resource, Pods, Object, External, ContainerResource).
	MetricType string `json:"metricType" yaml:"metricType"`
	// MetricName is the metric name (e.g., "cpu", "http_requests").
	MetricName string `json:"metricName" yaml:"metricName"`
	// Resource is the custom-metrics resource path segment (for example
	// "pods" or "deployments.apps").
	Resource string `json:"resource,omitempty" yaml:"resource,omitempty"`
	// ResourceName is the described object name, or "*" for a Pods metric.
	ResourceName string `json:"resourceName,omitempty" yaml:"resourceName,omitempty"`
	// ResourceNamespaced reports whether the custom-metrics resource is
	// namespace-scoped. Nil means the scope could not be resolved.
	ResourceNamespaced *bool `json:"resourceNamespaced,omitempty" yaml:"resourceNamespaced,omitempty"`
	// Selector is the label selector for Pods/Object metrics (if present).
	Selector string `json:"selector,omitempty" yaml:"selector,omitempty"`
	// TargetSelector is the scale target's pod selector. Resource and Pods
	// metric verification commands use it as labelSelector.
	TargetSelector string `json:"targetSelector,omitempty" yaml:"targetSelector,omitempty"`
	// TargetSelectorResolutionError explains why an exact pod-scoped
	// verification command could not be built.
	TargetSelectorResolutionError string `json:"targetSelectorResolutionError,omitempty" yaml:"targetSelectorResolutionError,omitempty"`
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
	// identity is populated by WithMetricContractIdentity when the caller has
	// the original autoscaling/v2 MetricSpec. Keeping it internal avoids
	// expanding the serialized report surface.
	identity MetricID
	// Type is the metric type (Resource, Pods, Object, External, ContainerResource).
	Type string
	// Name is the metric name (e.g., "cpu", "memory", "http_requests").
	Name string
	// Resource is the custom-metrics resource path segment.
	Resource string
	// ResourceName is the described object name, or "*" for a Pods metric.
	ResourceName string
	// ResourceNamespaced reports the discovered scope of a custom-metrics
	// resource. Nil means discovery could not resolve the resource.
	ResourceNamespaced *bool
	// ResourceResolutionError explains why an Object metric's described
	// resource could not be resolved through API discovery.
	ResourceResolutionError string
	// Selector is the label selector for Pods/Object metrics (if present).
	Selector string
	// TargetSelector is the discovered pod selector for the HPA scale target.
	TargetSelector string
	// TargetSelectorResolutionError explains why the scale target selector
	// could not be resolved.
	TargetSelectorResolutionError string
	// APIGroup is the metrics API group that should serve this metric.
	APIGroup string
	// HasCurrentData indicates whether current metric data exists in HPA status.
	HasCurrentData bool
}

// WithMetricContractIdentity attaches the canonical spec identity used for
// internal cross-analysis correlation. MetricContractMetric is an input DTO
// and the identity is not emitted in contract report JSON/YAML.
func WithMetricContractIdentity(metric MetricContractMetric, identity MetricID) MetricContractMetric {
	metric.identity = identity
	return metric
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
	anyUnresolvedResource := false

	for _, metric := range input.Metrics {
		check := analyzeMetric(metric, input.APIServices)
		report.Checks = append(report.Checks, check)

		switch check.Status {
		case "missing-api":
			anyMissingAPI = true
		case "missing-data", "selector-mismatch":
			anyMissingData = true
		case "unresolved-resource", "unresolved-target-selector":
			anyUnresolvedResource = true
		}
	}

	// Build overall status and remediation
	switch {
	case anyMissingAPI:
		report.OverallStatus = "broken"
		report.Summary = "Metrics API unavailable; HPA cannot compute desired replicas"
		report.Remediation = append(report.Remediation, "Install and configure the missing metrics adapter or metrics-server")
	case anyUnresolvedResource:
		report.OverallStatus = "degraded"
		report.Summary = "One or more metric references could not be resolved for exact verification"
		report.Remediation = append(report.Remediation, "Verify resource references, scale targets, and API discovery permissions")
	case anyMissingData:
		report.OverallStatus = "degraded"
		report.Summary = "Metrics APIs are available but not returning current data"
		report.Remediation = append(report.Remediation, "Verify the metric source is healthy and exporting data")
	default:
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
	apiSvc := metric.APIGroup
	if apiSvc == "" {
		apiSvc = metricsAPIForMetricType(metric.Type)
	}
	check := MetricContractCheck{
		identity:                      metric.identity,
		MetricType:                    metric.Type,
		MetricName:                    metric.Name,
		Resource:                      metric.Resource,
		ResourceName:                  metric.ResourceName,
		ResourceNamespaced:            metric.ResourceNamespaced,
		Selector:                      metric.Selector,
		TargetSelector:                metric.TargetSelector,
		TargetSelectorResolutionError: metric.TargetSelectorResolutionError,
		APIService:                    apiSvc,
		DataAvailable:                 metric.HasCurrentData,
	}
	if metric.HasCurrentData {
		check.DataMessage = "current data is present in HPA status"
	} else {
		check.DataMessage = "no current data in HPA status"
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

	if metric.ResourceResolutionError != "" {
		check.Status = "unresolved-resource"
		check.Detail = metric.ResourceResolutionError
		check.Remediation = "Verify the Object metric describedObject apiVersion/kind and grant API discovery access"
		return check
	}
	if metric.TargetSelectorResolutionError != "" {
		check.Status = "unresolved-target-selector"
		check.Detail = metric.TargetSelectorResolutionError
		check.Remediation = "Verify the scale target exists and grant permission to read its pod selector"
		return check
	}

	// Check data availability
	if !metric.HasCurrentData {
		check.Status = "missing-data"
		check.Detail = "Metrics API is available but not returning data"
		check.Remediation = fmt.Sprintf("Verify the %s metric source is healthy and exporting data", metric.Name)
		return check
	}

	check.Status = "ok"
	check.Detail = "Metric is available and current"
	return check
}

// metricsAPIForMetricType maps an HPA metric type to its metrics API.
func metricsAPIForMetricType(metricType string) string {
	switch metricType {
	case MetricTypeResource, "ContainerResource":
		return "metrics.k8s.io/v1beta1"
	case MetricTypePods:
		return "custom.metrics.k8s.io/v1beta1"
	case MetricTypeExternal:
		return "external.metrics.k8s.io/v1beta1"
	case MetricTypeObject:
		return "custom.metrics.k8s.io/v1beta1"
	default:
		return "unknown"
	}
}

// remediationForMissingAPI returns a remediation message for a missing metrics API.
func remediationForMissingAPI(apiService string) string {
	switch {
	case strings.HasPrefix(apiService, "metrics.k8s.io/"):
		return "Install metrics-server: kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml"
	case strings.HasPrefix(apiService, "custom.metrics.k8s.io/"):
		return "Install and configure a metrics adapter (e.g., Prometheus Adapter) for custom.metrics.k8s.io"
	case strings.HasPrefix(apiService, "external.metrics.k8s.io/"):
		return "Install and configure a metrics adapter (e.g., KEDA) for external.metrics.k8s.io"
	default:
		return fmt.Sprintf("Verify the %s APIService is installed and healthy", apiService)
	}
}
