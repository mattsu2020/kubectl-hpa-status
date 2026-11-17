// Package promhistory imports HPA scaling history from the Prometheus HTTP
// API (/api/v1/query_range) and synthesizes TimelineTrace records compatible
// with the timeline, analyze-record, and backtest pipelines.
package promhistory

import (
	"errors"
	"net/http"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// ErrNoPrometheusSeries indicates that Prometheus returned no matching HPA
// replica series for the requested namespace, name, and time window.
var ErrNoPrometheusSeries = errors.New("no matching HPA series returned by Prometheus")

// ErrAmbiguousSeries indicates that Prometheus returned multiple series where
// one HPA stream was expected (for example multiple clusters without a label
// filter), so the importer refuses to guess.
var ErrAmbiguousSeries = errors.New("ambiguous Prometheus series matched")

// Confidence levels describe how faithfully the imported trace represents
// historical HPA decisions.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// QueryConfig parameterizes a Prometheus range-query history import.
type QueryConfig struct {
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client

	Namespace string
	HPAName   string

	Start time.Time
	End   time.Time
	Step  time.Duration

	// Optional PromQL expression whose numeric samples populate
	// TimelineSnapshot.MetricValues. When empty, only replica counts and
	// conditions from kube-state-metrics are imported.
	MetricQuery  string
	MetricName   string
	MetricType   string
	MetricUnit   string
	MetricTarget float64
}

// ImportResult pairs the synthesized trace with explicit provenance,
// confidence, and non-fatal warnings so downstream consumers never treat
// reconstructed history as identical to live-recorded snapshots.
type ImportResult struct {
	Trace      hpaanalysis.TimelineTrace `json:"trace" yaml:"trace"`
	Provenance string                    `json:"provenance" yaml:"provenance"`
	Confidence string                    `json:"confidence" yaml:"confidence"`
	Warnings   []string                  `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}
