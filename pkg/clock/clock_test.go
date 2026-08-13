package clock

import (
	"testing"
	"time"
)

func TestSetForTest(t *testing.T) {
	fixed := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	restore := SetForTest(fixed)
	if got := Now(); !got.Equal(fixed) {
		t.Fatalf("Now() = %v, want %v", got, fixed)
	}
	restore()
	if got := Now(); got.Equal(fixed) {
		t.Fatal("restore left the process clock frozen")
	}
}
