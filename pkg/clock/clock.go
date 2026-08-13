// Package clock provides the process-wide, swappable time source used by
// domain and application packages. Services that need one consistent time per
// operation may still accept an injected Clock, but their production default
// should delegate to Now rather than defining another real-time source.
package clock

import (
	"sync/atomic"
	"time"
)

type clockFunc = func() time.Time

var holder atomic.Pointer[clockFunc]

func init() {
	realClock := clockFunc(time.Now)
	holder.Store(&realClock)
}

// Now returns the current time using the process-wide time source.
func Now() time.Time {
	clk := holder.Load()
	if clk == nil {
		return time.Now()
	}
	return (*clk)()
}

// SetForTest freezes the process-wide time source until the returned cleanup
// function is called. Concurrent tests must not install different clocks.
func SetForTest(t time.Time) (restore func()) {
	prev := holder.Load()
	frozen := clockFunc(func() time.Time { return t })
	holder.Store(&frozen)
	return func() {
		if prev == nil {
			live := clockFunc(time.Now)
			holder.Store(&live)
			return
		}
		holder.Store(prev)
	}
}
