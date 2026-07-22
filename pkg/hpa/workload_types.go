package hpa

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// ResourceCheckResult holds warnings about resource request/limit consistency with HPA targets.
type ResourceCheckResult struct {
	Warnings []ResourceWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// ResourceWarning describes a single resource consistency issue.
type ResourceWarning struct {
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Category  string `json:"category" yaml:"category"` // "missing-requests", "zero-requests", "target-vs-request-mismatch"
	Details   string `json:"details" yaml:"details"`
	Severity  string `json:"severity" yaml:"severity"` // "warning", "error"
}

// PodAnalysis holds per-pod readiness and resource analysis for HPA scale target pods.
type PodAnalysis struct {
	Total           int32              `json:"total" yaml:"total"`
	Ready           int32              `json:"ready" yaml:"ready"`
	Unready         int32              `json:"unready" yaml:"unready"`
	Pending         int32              `json:"pending" yaml:"pending"`
	Terminating     int32              `json:"terminating" yaml:"terminating"`
	ResourceIssues  []PodResourceIssue `json:"resourceIssues,omitempty" yaml:"resourceIssues,omitempty"`
	ContainerChecks []ContainerCheck   `json:"containerChecks,omitempty" yaml:"containerChecks,omitempty"`
}

// PodResourceIssue describes a pod container missing CPU or memory requests/limits.
type PodResourceIssue struct {
	Pod       string `json:"pod" yaml:"pod"`
	Container string `json:"container" yaml:"container"`
	Resource  string `json:"resource" yaml:"resource"`
	Category  string `json:"category" yaml:"category"` // "missing-request", "missing-limit"
}

// HealthSnapshot records a single health observation for trend tracking.
//
// Deprecated: Use healthtrend.HealthSnapshot instead. Scheduled for removal in v3.0.0.
type HealthSnapshot = healthtrend.HealthSnapshot

// HealthTrendResult holds the analysis of health score history over time.
//
// Deprecated: Use healthtrend.HealthTrendResult instead. Scheduled for removal in v3.0.0.
type HealthTrendResult = healthtrend.HealthTrendResult

// ContainerCheck verifies that a ContainerResource metric target container exists in pods.
type ContainerCheck struct {
	Container string `json:"container" yaml:"container"`
	Found     bool   `json:"found" yaml:"found"`
	Message   string `json:"message,omitempty" yaml:"message,omitempty"`
}
