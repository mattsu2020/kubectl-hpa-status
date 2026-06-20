package clock

import (
	"testing"
	"time"
)

func TestNow_DefaultsToRealTime(t *testing.T) {
	before := time.Now()
	got := Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Fatalf("Now() = %v, expected within [%v, %v]", got, before, after)
	}
}

func TestSetForTest_ReplacesAndRestoresClock(t *testing.T) {
	frozen := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)

	realAtStart := Now()
	restore := SetForTest(frozen)
	if got := Now(); !got.Equal(frozen) {
		t.Fatalf("Now() after SetForTest = %v, want %v", got, frozen)
	}

	restore()

	restored := Now()
	if restored.Equal(frozen) {
		t.Fatalf("clock not restored: Now() still returns frozen time %v", frozen)
	}
	if restored.Before(realAtStart.Add(-time.Hour)) {
		t.Fatalf("restored Now() = %v looks unrealistic relative to start %v", restored, realAtStart)
	}
}

func TestSetForTest_RestoreIsIdempotentSafe(t *testing.T) {
	// Restoring twice must not panic and must leave the clock functional.
	frozen := time.Date(2025, time.June, 20, 12, 0, 0, 0, time.UTC)
	restore := SetForTest(frozen)
	restore()
	restore()

	if got := Now(); got.IsZero() {
		t.Fatalf("Now() returned zero time after double restore")
	}
}
