package hpa

// StatusBatchStatus is the per-item outcome of a multi-HPA status run.
//
// Serialization values are lowercase to distinguish them from HealthState
// values ("OK"/"ERROR"/"LIMITED"/"STABILIZED"): a StatusBatchItem describes
// whether we were able to produce a report at all, while Health describes
// the HPA's health among successfully produced reports.
type StatusBatchStatus string

const (
	// BatchStatusOK indicates a report was produced and the HPA is healthy.
	BatchStatusOK StatusBatchStatus = "ok"
	// BatchStatusWarning indicates a report was produced but health is ERROR/LIMITED/WARNING.
	BatchStatusWarning StatusBatchStatus = "warning"
	// BatchStatusError indicates a report could not be produced (fetch/build failure).
	BatchStatusError StatusBatchStatus = "error"
)

// StatusBatch is the envelope for multi-HPA status output. It replaces the
// historical bare []StatusReport array so that per-item failures can be
// surfaced alongside successes instead of aborting the whole batch.
//
// Shape (JSON):
//
//	{
//	  "apiVersion": "hpa-status/v1",
//	  "items": [
//	    {"namespace": "...", "name": "...", "status": "ok", "report": {...}},
//	    {"namespace": "...", "name": "...", "status": "error", "error": "..."}
//	  ]
//	}
type StatusBatch struct {
	// APIVersion identifies the JSON/YAML schema version (see SchemaVersion).
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Items      []StatusBatchItem `json:"items" yaml:"items"`
}

// StatusBatchItem is one HPA's outcome in a multi-HPA run.
//
// For successful items Status is "ok" or "warning" and Report is populated.
// For failed items Status is "error", Error carries the failure message, and
// Report is nil. Namespace/Name are always populated so consumers can correlate
// items to the input order even when the report is missing.
type StatusBatchItem struct {
	Namespace string            `json:"namespace" yaml:"namespace"`
	Name      string            `json:"name" yaml:"name"`
	Status    StatusBatchStatus `json:"status" yaml:"status"`
	// Error is the failure message for items with Status == "error".
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
	// Report is the full status report for items with Status != "error".
	Report *StatusReport `json:"report,omitempty" yaml:"report,omitempty"`
}
