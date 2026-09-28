package hpa

import "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/errs"

// Sentinel errors for analysis and simulation failures. Wrap these with
// fmt.Errorf("...: %w", ErrXxx) at the call site so callers can match on the
// concrete condition with errors.Is instead of substring matching on the
// English message text.
//
// These are part of the public pkg/hpa API contract: downstream tools importing
// the analysis model can branch on them. The values are aliases of the shared
// pkg/hpa/internal/errs sentinels so errors.Is matches uniformly across the
// hpa root package and pkg/hpa/simulate. Do not remove them without bumping
// the module major version.
var (
	// ErrNilHPA is returned when an analysis/simulation function is invoked
	// with a nil *HorizontalPodAutoscaler.
	ErrNilHPA = errs.ErrNilHPA

	// ErrNilReport is returned when a report-rendering function is invoked
	// with a nil report pointer.
	ErrNilReport = errs.ErrNilReport

	// ErrMetricNotFound is returned when a simulation override references a
	// metric name that does not appear in the HPA spec.
	ErrMetricNotFound = errs.ErrMetricNotFound

	// ErrMetricAmbiguous is returned when a name-only metric reference matches
	// more than one canonical MetricID.
	ErrMetricAmbiguous = errs.ErrMetricAmbiguous
)
