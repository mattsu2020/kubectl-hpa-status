package hpa

import "time"

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
type HealthSnapshot struct {
	Timestamp       time.Time `json:"timestamp" yaml:"timestamp"`
	HealthScore     int       `json:"healthScore" yaml:"healthScore"`
	HealthState     string    `json:"healthState" yaml:"healthState"`
	DesiredReplicas int32     `json:"desiredReplicas" yaml:"desiredReplicas"`
	CurrentReplicas int32     `json:"currentReplicas" yaml:"currentReplicas"`
	Stabilizing     bool      `json:"stabilizing,omitempty" yaml:"stabilizing,omitempty"`
}

// HealthTrendResult holds the analysis of health score history over time.
type HealthTrendResult struct {
	Snapshots        []HealthSnapshot   `json:"snapshots" yaml:"snapshots"`
	Variance         float64            `json:"variance" yaml:"variance"`
	MinScore         int                `json:"minScore" yaml:"minScore"`
	MaxScore         int                `json:"maxScore" yaml:"maxScore"`
	MeanScore        float64            `json:"meanScore" yaml:"meanScore"`
	DegradationRate  float64            `json:"degradationRate" yaml:"degradationRate"`
	FlappingDetected bool               `json:"flappingDetected" yaml:"flappingDetected"`
	FlappingSeverity string             `json:"flappingSeverity,omitempty" yaml:"flappingSeverity,omitempty"`
	Sparkline        string             `json:"sparkline,omitempty" yaml:"sparkline,omitempty"`
	Anomalies        []AnomalyDetection `json:"anomalies,omitempty" yaml:"anomalies,omitempty"`
}

// ContainerCheck verifies that a ContainerResource metric target container exists in pods.
type ContainerCheck struct {
	Container string `json:"container" yaml:"container"`
	Found     bool   `json:"found" yaml:"found"`
	Message   string `json:"message,omitempty" yaml:"message,omitempty"`
}
