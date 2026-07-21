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
