package hpa

import "errors"

// Sentinel errors for analysis and simulation failures. Wrap these with
// fmt.Errorf("...: %w", ErrXxx) at the call site so callers can match on the
// concrete condition with errors.Is instead of substring matching on the
// English message text.
var (
	// ErrNilHPA is returned when an analysis/simulation function is invoked
	// with a nil *HorizontalPodAutoscaler.
	ErrNilHPA = errors.New("HPA must not be nil")

	// ErrNilReport is returned when a report-rendering function is invoked
	// with a nil report pointer.
	ErrNilReport = errors.New("report is nil")

	// ErrMetricNotFound is returned when a simulation override references a
	// metric name that does not appear in the HPA spec.
	ErrMetricNotFound = errors.New("metric not found in HPA spec")
)
