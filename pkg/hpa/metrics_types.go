package hpa

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricsPipelineDiagnostics holds the results of metrics pipeline health checks.
type MetricsPipelineDiagnostics struct {
	OverallStatus    string                 `json:"overallStatus" yaml:"overallStatus"`
	PerMetricChecks  []PerMetricHealthCheck `json:"perMetricChecks,omitempty" yaml:"perMetricChecks,omitempty"`
	RemediationSteps []string               `json:"remediationSteps,omitempty" yaml:"remediationSteps,omitempty"`
}

// PerMetricHealthCheck describes the health of a single metric source.
type PerMetricHealthCheck struct {
	MetricType  string `json:"metricType" yaml:"metricType"`
	MetricName  string `json:"metricName" yaml:"metricName"`
	Selector    string `json:"selector,omitempty" yaml:"selector,omitempty"`
	Status      string `json:"status" yaml:"status"` // "healthy", "missing", "stale"
	Details     string `json:"details,omitempty" yaml:"details,omitempty"`
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// AdapterDiagnosticsReport summarizes visible custom/external metrics adapter health.
type AdapterDiagnosticsReport struct {
	AdapterType          string                `json:"adapterType" yaml:"adapterType"`
	EndpointHealthy      bool                  `json:"endpointHealthy" yaml:"endpointHealthy"`
	AvailableMetrics     []string              `json:"availableMetrics,omitempty" yaml:"availableMetrics,omitempty"`
	AuthenticationErrors []string              `json:"authenticationErrors,omitempty" yaml:"authenticationErrors,omitempty"`
	QueryProposals       []MetricQueryProposal `json:"queryProposals,omitempty" yaml:"queryProposals,omitempty"`
	Checks               []AdapterCheck        `json:"checks,omitempty" yaml:"checks,omitempty"`
	Summary              string                `json:"summary" yaml:"summary"`
}

// AdapterCheck describes one visible adapter diagnostic check.
type AdapterCheck struct {
	Name        string `json:"name" yaml:"name"`
	Status      string `json:"status" yaml:"status"`
	Details     string `json:"details,omitempty" yaml:"details,omitempty"`
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// MetricQueryProposal suggests a metrics API query for troubleshooting.
type MetricQueryProposal struct {
	MetricName    string `json:"metricName" yaml:"metricName"`
	ProposedQuery string `json:"proposedQuery" yaml:"proposedQuery"`
	Adapter       string `json:"adapter" yaml:"adapter"`
}

// MetricFreshnessStatus represents the freshness state of a single HPA metric.
type MetricFreshnessStatus string

const (
	// FreshnessOK means the metric has recent data available.
	FreshnessOK MetricFreshnessStatus = "OK"
	// FreshnessStale means the metric data is older than expected.
	FreshnessStale MetricFreshnessStatus = "Stale"
	// FreshnessMissing means the metric has no current data in HPA status.
	FreshnessMissing MetricFreshnessStatus = "Missing"
	// FreshnessUnknown means freshness cannot be determined.
	FreshnessUnknown MetricFreshnessStatus = "Unknown"
)

// MetricFreshness holds the freshness analysis for a single HPA metric.
type MetricFreshness struct {
	// Name is the metric display name (e.g., "cpu", "queue_depth").
	Name string `json:"name" yaml:"name"`
	// Type is the metric source type (Resource, Pods, Object, External, ContainerResource).
	Type string `json:"type" yaml:"type"`
	// Status is the freshness state: OK, Stale, Missing, Unknown.
	Status string `json:"status" yaml:"status"`
	// LastSeen is the timestamp when the metric was last observed, if available.
	LastSeen *metav1.Time `json:"lastSeen,omitempty" yaml:"lastSeen,omitempty"`
	// Age is the duration since LastSeen. Zero if LastSeen is nil.
	Age time.Duration `json:"age,omitempty" yaml:"age,omitempty"`
	// Source is the metrics API serving this metric (e.g., metrics.k8s.io,
	// custom.metrics.k8s.io, external.metrics.k8s.io).
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Window is the expected metric collection window (e.g., "30s" for resource metrics).
	Window string `json:"window,omitempty" yaml:"window,omitempty"`
	// APIServiceAvailable records whether the backing metrics API was visible
	// through Kubernetes API discovery at analysis time.
	APIServiceAvailable *bool `json:"apiServiceAvailable,omitempty" yaml:"apiServiceAvailable,omitempty"`
	// APIServiceMessage explains API discovery or APIService availability evidence.
	APIServiceMessage string `json:"apiServiceMessage,omitempty" yaml:"apiServiceMessage,omitempty"`
	// LastEvent is the latest HPA event related to this metric, if one was visible.
	LastEvent *Event `json:"lastEvent,omitempty" yaml:"lastEvent,omitempty"`
	// Risk describes the HPA behavior risk from stale/missing data.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
	// Evidence lists observed signals supporting the freshness status.
	Evidence []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	// NextSteps lists kubectl commands or actions for remediation.
	NextSteps []string `json:"nextSteps,omitempty" yaml:"nextSteps,omitempty"`
}
