package hpa

import (
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/clock"
)

// This file re-exports the swappable clock from pkg/clock so the
// existing unexported now() wrapper and exported SetClockForTest keep working
// without changing any call site in pkg/hpa. Sub-packages that need the
// current time should import pkg/clock directly.

// now returns the current time using the package's swappable clock. In
// production this delegates to time.Now; tests can inject a frozen clock via
// SetClockForTest.
func now() time.Time {
	return clock.Now()
}

// setClockForTest replaces the package clock for the duration of a test and
// returns a cleanup function that restores the real clock. Tests MUST defer
// the cleanup so that subsequent (possibly parallel) tests observe real time.
//
// Example:
//
//	defer setClockForTest(fixedTime)()
//
// This helper delegates to clock.SetForTest. It is safe under t.Parallel()
// within a single test, but tests must not run setClockForTest concurrently
// with each other: the clock is a single package-level value and the last
// writer wins.
func setClockForTest(t time.Time) (restore func()) {
	return clock.SetForTest(t)
}
