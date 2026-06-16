package hpa

import (
	"sync/atomic"
	"time"
)

// clockFunc is the type stored in the atomic clock pointer. Keeping it as a
// named type makes the atomic operations easier to read.
type clockFunc = func() time.Time

// clockHolder stores the current time source atomically. Production code reads
// it via now(); tests can replace it via SetClockForTest (and must restore it
// via the returned cleanup function). The atomic load/store makes concurrent
// reads safe even while a test in another goroutine swaps the clock.
var clockHolder atomic.Pointer[clockFunc]

func init() {
	realClock := clockFunc(time.Now)
	clockHolder.Store(&realClock)
}

// now returns the current time using the package's swappable clock. In
// production this delegates to time.Now; tests can inject a frozen clock via
// SetClockForTest.
func now() time.Time {
	clk := clockHolder.Load()
	if clk == nil {
		return time.Now()
	}
	return (*clk)()
}

// SetClockForTest replaces the package clock for the duration of a test and
// returns a cleanup function that restores the real clock. Tests MUST defer
// the cleanup so that subsequent (possibly parallel) tests observe real time.
//
// Example:
//
//	defer SetClockForTest(fixedTime)()
//
// This helper is safe under t.Parallel() within a single test, but tests must
// not run SetClockForTest concurrently with each other: the clock is a single
// package-level value and the last writer wins. The recommended pattern is to
// keep clock swaps local to one test function and always restore via cleanup.
func SetClockForTest(t time.Time) (restore func()) {
	prev := clockHolder.Load()
	frozen := clockFunc(func() time.Time { return t })
	clockHolder.Store(&frozen)
	return func() {
		if prev == nil {
			live := clockFunc(time.Now)
			clockHolder.Store(&live)
		} else {
			clockHolder.Store(prev)
		}
	}
}
