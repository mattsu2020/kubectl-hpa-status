package hpa

import "time"

// MetricReading is the typed numeric form of one metric status captured at
// record time. Records written before typed metric capture carry no entries;
// the formatted TopMetric string remains as the human-readable fallback.
type MetricReading struct {
	// Type is the metric source: Resource, ContainerResource, Pods, Object,
	// or External.
	Type string `json:"type" yaml:"type"`
	// Name identifies the metric within its source (container-qualified for
	// ContainerResource metrics).
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// Value is the numeric reading: utilization percent, or the canonical
	// decimal value of the quantity.
	Value float64 `json:"value" yaml:"value"`
	// Target is the numeric spec target when one is configured.
	Target float64 `json:"target,omitempty" yaml:"target,omitempty"`
	// Unit qualifies Value/Target: "%" for utilization, "" for canonical
	// decimal quantities.
	Unit string `json:"unit,omitempty" yaml:"unit,omitempty"`
}

// TimelineSnapshot captures the state of an HPA at a single point in time.
type TimelineSnapshot struct {
	Timestamp      time.Time       `json:"timestamp" yaml:"timestamp"`
	Current        int32           `json:"currentReplicas" yaml:"currentReplicas"`
	Desired        int32           `json:"desiredReplicas" yaml:"desiredReplicas"`
	Health         string          `json:"health" yaml:"health"`
	HealthScore    int             `json:"healthScore" yaml:"healthScore"`
	TopMetric      string          `json:"topMetric" yaml:"topMetric"`
	MetricValues   []MetricReading `json:"metricValues,omitempty" yaml:"metricValues,omitempty"`
	Conditions     []Condition     `json:"conditions" yaml:"conditions"`
	Summary        string          `json:"summary" yaml:"summary"`
	Interpretation []string        `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	Events         []Event         `json:"events,omitempty" yaml:"events,omitempty"`
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
