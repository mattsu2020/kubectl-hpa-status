package retrospective

import "time"

// Entry represents a single estimated scaling decision event reconstructed
// from Kubernetes events and HPA status signals.
type Entry struct {
	Timestamp  time.Time `json:"timestamp" yaml:"timestamp"`
	Category   string    `json:"category" yaml:"category"`
	Message    string    `json:"message" yaml:"message"`
	Source     string    `json:"source" yaml:"source"`
	Confidence string    `json:"confidence,omitempty" yaml:"confidence,omitempty"`
}

// Timeline holds the result of reconstructing past scaling decisions from
// Kubernetes events and current HPA status.
type Timeline struct {
	HPAName    string    `json:"hpaName" yaml:"hpaName"`
	Namespace  string    `json:"namespace" yaml:"namespace"`
	Since      time.Time `json:"since" yaml:"since"`
	Until      time.Time `json:"until" yaml:"until"`
	Entries    []Entry   `json:"entries" yaml:"entries"`
	Disclaimer string    `json:"disclaimer" yaml:"disclaimer"`
	Warnings   []string  `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}
