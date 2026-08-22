package hpa

// SchemaVersionV2 identifies the additive nested output projection. The
// in-memory Analysis type remains the v1 compatibility model; V2 projection
// keeps breaking JSON changes out of domain calculations.
const SchemaVersionV2 = "hpa-status/v2"

// StatusRecordStatusV2 is the stable outcome enum used by v2 JSONL records.
// It describes whether a report was produced, independently from the HPA
// health value nested inside that report.
type StatusRecordStatusV2 string

const (
	// StatusRecordSuccessV2 indicates that a report was produced successfully.
	StatusRecordSuccessV2 StatusRecordStatusV2 = "success"
	// StatusRecordWarningV2 indicates that a report was produced but its HPA
	// health contributes a warning exit status.
	StatusRecordWarningV2 StatusRecordStatusV2 = "warning"
	// StatusRecordErrorV2 indicates that report construction failed.
	StatusRecordErrorV2 StatusRecordStatusV2 = "error"
)

// StatusReportV2 is the nested status output selected explicitly by clients.
type StatusReportV2 struct {
	APIVersion string          `json:"apiVersion" yaml:"apiVersion"`
	Analysis   GroupedAnalysis `json:"analysis" yaml:"analysis"`
	Events     []Event         `json:"events,omitempty" yaml:"events,omitempty"`
}

// StatusBatchV2 is the multi-HPA envelope for nested reports.
type StatusBatchV2 struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Items      []StatusBatchItemV2 `json:"items" yaml:"items"`
}

// StatusBatchItemV2 mirrors StatusBatchItem while carrying a nested report.
type StatusBatchItemV2 struct {
	Namespace string            `json:"namespace" yaml:"namespace"`
	Name      string            `json:"name" yaml:"name"`
	Status    StatusBatchStatus `json:"status" yaml:"status"`
	Error     string            `json:"error,omitempty" yaml:"error,omitempty"`
	Report    *StatusReportV2   `json:"report,omitempty" yaml:"report,omitempty"`
}

// StatusRecordV2 is the canonical one-record-per-line v2 JSONL envelope.
// Successful and warning records carry Report; failed records carry Error.
// Keeping the envelope identical for single and multi-HPA commands lets
// streaming consumers decode every line into one stable type.
type StatusRecordV2 struct {
	APIVersion string               `json:"apiVersion" yaml:"apiVersion"`
	Namespace  string               `json:"namespace" yaml:"namespace"`
	Name       string               `json:"name" yaml:"name"`
	Status     StatusRecordStatusV2 `json:"status" yaml:"status"`
	Error      string               `json:"error,omitempty" yaml:"error,omitempty"`
	Report     *StatusReportV2      `json:"report,omitempty" yaml:"report,omitempty"`
}

// ProjectStatusReportV2 projects a v1 compatibility report into nested v2
// output without mutating the source.
func ProjectStatusReportV2(report StatusReport) StatusReportV2 {
	return StatusReportV2{
		APIVersion: SchemaVersionV2,
		Analysis:   report.CanonicalAnalysis(),
		Events:     cloneEvents(report.Events),
	}
}

// ProjectStatusReportsV2 preserves input order while projecting reports.
func ProjectStatusReportsV2(reports []StatusReport) []StatusReportV2 {
	if len(reports) == 0 {
		return nil
	}
	projected := make([]StatusReportV2, len(reports))
	for i := range reports {
		projected[i] = ProjectStatusReportV2(reports[i])
	}
	return projected
}

// ProjectStatusRecordV2 wraps one successful v1 report in the stable v2 JSONL
// record envelope.
func ProjectStatusRecordV2(report StatusReport) StatusRecordV2 {
	projected := ProjectStatusReportV2(report)
	return StatusRecordV2{
		APIVersion: SchemaVersionV2,
		Namespace:  report.Analysis.Meta.Namespace,
		Name:       report.Analysis.Meta.Name,
		Status:     statusRecordReportStatusV2(report),
		Report:     &projected,
	}
}

// ProjectStatusRecordsV2 projects a v1 batch into canonical JSONL records,
// preserving item order and partial failures.
func ProjectStatusRecordsV2(batch StatusBatch) []StatusRecordV2 {
	if len(batch.Items) == 0 {
		return nil
	}
	records := make([]StatusRecordV2, len(batch.Items))
	for i := range batch.Items {
		item := batch.Items[i]
		if item.Report != nil {
			record := ProjectStatusRecordV2(*item.Report)
			record.Namespace = item.Namespace
			record.Name = item.Name
			records[i] = record
			continue
		}
		errorMessage := item.Error
		if errorMessage == "" {
			errorMessage = "status report unavailable"
		}
		records[i] = StatusRecordV2{
			APIVersion: SchemaVersionV2,
			Namespace:  item.Namespace,
			Name:       item.Name,
			Status:     StatusRecordErrorV2,
			Error:      errorMessage,
		}
	}
	return records
}

func statusRecordReportStatusV2(report StatusReport) StatusRecordStatusV2 {
	switch HealthState(report.Analysis.Decision.Health) {
	case HealthError, HealthLimited:
		return StatusRecordWarningV2
	default:
		if report.Analysis.Decision.Health == "WARNING" {
			return StatusRecordWarningV2
		}
		return StatusRecordSuccessV2
	}
}

// ProjectStatusBatchV2 preserves successful and failed item outcomes.
func ProjectStatusBatchV2(batch StatusBatch) StatusBatchV2 {
	items := make([]StatusBatchItemV2, len(batch.Items))
	for i := range batch.Items {
		source := batch.Items[i]
		items[i] = StatusBatchItemV2{
			Namespace: source.Namespace,
			Name:      source.Name,
			Status:    source.Status,
			Error:     source.Error,
		}
		if source.Report != nil {
			report := ProjectStatusReportV2(*source.Report)
			items[i].Report = &report
		}
	}
	return StatusBatchV2{APIVersion: SchemaVersionV2, Items: items}
}

func cloneEvents(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	return append([]Event(nil), events...)
}
