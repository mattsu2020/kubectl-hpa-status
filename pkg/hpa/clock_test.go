package hpa

import (
	"testing"
	"time"
)

func TestSetClockForTest_FreezesAndRestores(t *testing.T) {
	frozen := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	// Before freezing, now() tracks real time.
	before := time.Now()
	realNow := now()
	if realNow.Sub(before) < -time.Second || realNow.Sub(before) > time.Second {
		t.Fatalf("real-time now() drifted too far: before=%v now=%v", before, realNow)
	}

	restore := SetClockForTest(frozen)
	defer restore()

	// While frozen, now() must return the exact fixed time.
	for i := 0; i < 3; i++ {
		if got := now(); !got.Equal(frozen) {
			t.Fatalf("frozen now() = %v, want %v", got, frozen)
		}
	}

	restore()

	// After restoring, now() must track real time again.
	after := time.Now()
	restoredNow := now()
	if restoredNow.Sub(after) < -time.Second || restoredNow.Sub(after) > time.Second {
		t.Fatalf("restored now() drifted too far: after=%v now=%v", after, restoredNow)
	}
}

func TestSetClockForTest_IsCallableConcurrently(_ *testing.T) {
	// This test documents the documented contract: reads of now() are safe
	// under concurrency. It does not swap the clock concurrently (that is
	// explicitly disallowed), but many goroutines reading now() must not race.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = now()
		}
	}()
	// Meanwhile the main goroutine also reads.
	for i := 0; i < 100; i++ {
		_ = now()
	}
	<-done
}
