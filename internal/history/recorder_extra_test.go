package history

import (
	"strings"
	"testing"
	"time"
)

// TestRecorderNilStoreExposesWarning covers the nil-store guard in
// RecordAndAnalyze.
func TestRecorderNilStoreExposesWarning(t *testing.T) {
	r := NewRecorder(nil, nil)
	result := r.RecordAndAnalyze(RecordInput{Namespace: "ns", Name: "app"})
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %+v", len(result.Warnings), result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "unavailable") {
		t.Fatalf("unexpected warning: %q", result.Warnings[0])
	}
}

// TestRecorderRealClockAndDefaultClock covers the realClock.Now() path and the
// nil-clock default in NewRecorder. It uses a real in-memory-or-disk store so
// the full RecordAndAnalyze pipeline runs against the real clock.
func TestRecorderRealClockAndDefaultClock(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}
	r := NewRecorder(store, nil) // nil clock -> realClock
	result := r.RecordAndAnalyze(RecordInput{
		Namespace: "ns", Name: "app",
		HealthScore: 90, HealthState: "OK", DesiredReplicas: 3, CurrentReplicas: 3,
		Since: 1 * time.Hour, Retention: 24 * time.Hour,
	})
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", result.Warnings)
	}
	if result.Trend == nil {
		t.Fatalf("expected a trend to be computed from the recorded snapshot")
	}
}
