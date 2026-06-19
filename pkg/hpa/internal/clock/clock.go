// Package clock provides a swappable time source for pkg/hpa and its
// sub-packages. Production code reads the current time via Now; tests inject
// a frozen clock via SetForTest (and must restore it via the returned cleanup
// function).
//
// pkg/hpa re-exports Now through its own unexported now() wrapper so the
// existing call sites in pkg/hpa keep working unchanged. Sub-packages that
// need the current time import this package directly.
package clock

import (
	"sync/atomic"
	"time"
)

// clockFunc is the type stored in the atomic clock pointer.
type clockFunc = func() time.Time

// holder stores the current time source atomically. Production code reads it
// via Now(); tests can replace it via SetForTest (and must restore it via the
// returned cleanup function). The atomic load/store makes concurrent reads
// safe even while a test in another goroutine swaps the clock.
var holder atomic.Pointer[clockFunc]

func init() {
	realClock := clockFunc(time.Now)
	holder.Store(&realClock)
}

// Now returns the current time using the package's swappable clock. In
// production this delegates to time.Now; tests can inject a frozen clock via
// SetForTest.
func Now() time.Time {
	clk := holder.Load()
	if clk == nil {
		return time.Now()
	}
	return (*clk)()
}

// SetForTest replaces the package clock for the duration of a test and returns
// a cleanup function that restores the real clock. Tests MUST defer the
// cleanup so that subsequent (possibly parallel) tests observe real time.
//
// Example:
//
//	defer clock.SetForTest(fixedTime)()
//
// This helper is safe under t.Parallel() within a single test, but tests must
// not run SetForTest concurrently with each other: the clock is a single
// package-level value and the last writer wins. The recommended pattern is to
// keep clock swaps local to one test function and always restore via cleanup.
func SetForTest(t time.Time) (restore func()) {
	prev := holder.Load()
	frozen := clockFunc(func() time.Time { return t })
	holder.Store(&frozen)
	return func() {
		if prev == nil {
			live := clockFunc(time.Now)
			holder.Store(&live)
		} else {
			holder.Store(prev)
		}
	}
}
