package hpa

import "time"

// TimelineSnapshot captures the state of an HPA at a single point in time.
type TimelineSnapshot struct {
	Timestamp      time.Time   `json:"timestamp" yaml:"timestamp"`
	Current        int32       `json:"currentReplicas" yaml:"currentReplicas"`
	Desired        int32       `json:"desiredReplicas" yaml:"desiredReplicas"`
	Health         string      `json:"health" yaml:"health"`
	HealthScore    int         `json:"healthScore" yaml:"healthScore"`
	TopMetric      string      `json:"topMetric" yaml:"topMetric"`
	Conditions     []Condition `json:"conditions" yaml:"conditions"`
	Summary        string      `json:"summary" yaml:"summary"`
	Interpretation []string    `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	Events         []Event     `json:"events,omitempty" yaml:"events,omitempty"`
}

// TimelineTrace holds a sequence of snapshots for a single HPA.
type TimelineTrace struct {
	HPAName   string             `json:"hpaName" yaml:"hpaName"`
	Namespace string             `json:"namespace" yaml:"namespace"`
	Start     time.Time          `json:"start" yaml:"start"`
	End       time.Time          `json:"end,omitempty" yaml:"end,omitempty"`
	Interval  time.Duration      `json:"interval" yaml:"interval"`
	Snapshots []TimelineSnapshot `json:"snapshots" yaml:"snapshots"`
}

// RetrospectiveEntry represents a single estimated scaling decision event
// reconstructed from Kubernetes events and HPA status signals.
type RetrospectiveEntry struct {
	Timestamp  time.Time `json:"timestamp" yaml:"timestamp"`
	Category   string    `json:"category" yaml:"category"`
	Message    string    `json:"message" yaml:"message"`
	Source     string    `json:"source" yaml:"source"`
	Confidence string    `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// RetrospectiveTimeline holds the result of reconstructing past scaling decisions
// from Kubernetes events and current HPA status.
type RetrospectiveTimeline struct {
	HPAName    string               `json:"hpaName" yaml:"hpaName"`
	Namespace  string               `json:"namespace" yaml:"namespace"`
	Since      time.Time            `json:"since" yaml:"since"`
	Until      time.Time            `json:"until" yaml:"until"`
	Entries    []RetrospectiveEntry `json:"entries" yaml:"entries"`
	Disclaimer string               `json:"disclaimer" yaml:"disclaimer"`
	Warnings   []string             `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}
