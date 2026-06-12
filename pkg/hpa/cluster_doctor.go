package hpa

import "fmt"

// ClusterDiagnostics holds the result of cluster-level prerequisite checks
// for HPA functionality. This covers metrics API services, metrics-server
// health, and RBAC permissions.
type ClusterDiagnostics struct {
	// APIServices lists the status of metrics API services.
	APIServices []APIServiceCheck `json:"apiServices" yaml:"apiServices"`
	// MetricsServer holds metrics-server deployment health.
	MetricsServer *MetricsServerCheck `json:"metricsServer,omitempty" yaml:"metricsServer,omitempty"`
	// RBAC holds the result of RBAC permission checks.
	RBAC *RBACCheckResult `json:"rbac,omitempty" yaml:"rbac,omitempty"`
	// Summary is a human-readable summary of the diagnostics.
	Summary string `json:"summary" yaml:"summary"`
	// OverallStatus is one of "healthy", "degraded", "unhealthy".
	OverallStatus string `json:"overallStatus" yaml:"overallStatus"`
}

// APIServiceCheck holds the status of a single metrics API service.
type APIServiceCheck struct {
	// Name is the API service group/version (e.g. "metrics.k8s.io/v1beta1").
	Name string `json:"name" yaml:"name"`
	// Status is "available", "unavailable", or "unknown".
	Status string `json:"status" yaml:"status"`
	// Message provides additional context (e.g. error details).
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// MetricsServerCheck holds the health status of the metrics-server deployment.
type MetricsServerCheck struct {
	// Available indicates whether metrics-server is running.
	Available bool `json:"available" yaml:"available"`
	// Ready indicates whether all replicas are ready.
	Ready bool `json:"ready" yaml:"ready"`
	// Replicas is the total number of metrics-server replicas.
	Replicas int32 `json:"replicas" yaml:"replicas"`
	// ReadyReplicas is the number of ready metrics-server replicas.
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	// Namespace is the namespace where metrics-server is deployed.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Version is the metrics-server image version (if detectable).
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	// Message provides additional context.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// RBACCheckResult holds the result of SelfSubjectAccessReview checks.
type RBACCheckResult struct {
	// CanGetHPA indicates whether the user can get HPAs.
	CanGetHPA bool `json:"canGetHpa" yaml:"canGetHpa"`
	// CanListHPA indicates whether the user can list HPAs.
	CanListHPA bool `json:"canListHpa" yaml:"canListHpa"`
	// CanGetPods indicates whether the user can get pods.
	CanGetPods bool `json:"canGetPods" yaml:"canGetPods"`
	// CanGetEvents indicates whether the user can get events.
	CanGetEvents bool `json:"canGetEvents" yaml:"canGetEvents"`
	// Message provides additional context.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// BuildClusterDiagnosticsSummary generates a human-readable summary and
// overall status from the diagnostic results.
func BuildClusterDiagnosticsSummary(d *ClusterDiagnostics) {
	unhealthyServices := 0
	for _, svc := range d.APIServices {
		if svc.Status != "available" {
			unhealthyServices++
		}
	}

	rbacIssues := false
	if d.RBAC != nil {
		rbacIssues = !d.RBAC.CanGetHPA || !d.RBAC.CanListHPA || !d.RBAC.CanGetPods
	}

	metricsServerIssue := d.MetricsServer != nil && !d.MetricsServer.Available

	switch {
	case unhealthyServices > 0 || metricsServerIssue || rbacIssues:
		d.OverallStatus = "unhealthy"
		var parts []string
		if unhealthyServices > 0 {
			parts = append(parts, fmt.Sprintf("%d API service(s) unavailable", unhealthyServices))
		}
		if metricsServerIssue {
			parts = append(parts, "metrics-server not available")
		}
		if rbacIssues {
			parts = append(parts, "insufficient RBAC permissions")
		}
		d.Summary = "Cluster prerequisites are NOT met: " + joinWithComma(parts) + ". HPA diagnostics may be incomplete."
	default:
		d.OverallStatus = "healthy"
		d.Summary = "All cluster prerequisites are met. HPA diagnostics should work correctly."
	}
}

func joinWithComma(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += ", " + parts[i]
	}
	return result
}
