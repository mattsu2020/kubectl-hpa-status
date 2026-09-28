// Package errs holds the sentinel errors shared by the hpa root package and
// the simulate package. Both packages used to declare their own copies with
// identical messages, so an error wrapped by simulate never matched the root
// package's sentinel via errors.Is. Declaring each sentinel once here and
// aliasing it from both packages keeps errors.Is working uniformly.
package errs

import "errors"

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

	// ErrMetricAmbiguous is returned when a name-only metric reference matches
	// more than one canonical MetricID.
	ErrMetricAmbiguous = errors.New("metric name is ambiguous")
)
