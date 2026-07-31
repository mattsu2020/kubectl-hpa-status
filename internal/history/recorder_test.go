package history

import (
	"errors"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeSnapshotStore struct {
	appended healthtrend.HealthSnapshot
	loadNow  time.Time
	pruneNow time.Time
	loadErr  error
}

func (s *fakeSnapshotStore) Append(_, _ string, snapshot healthtrend.HealthSnapshot) error {
	s.appended = snapshot
	return nil
}

func (s *fakeSnapshotStore) LoadAt(_, _ string, _ time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	s.loadNow = now
	return []healthtrend.HealthSnapshot{s.appended}, s.loadErr
}

func (s *fakeSnapshotStore) PruneAt(_, _ string, _ time.Duration, now time.Time) error {
	s.pruneNow = now
	return nil
}

func TestRecorderUsesOneClockAndReturnsTrend(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	store := &fakeSnapshotStore{}
	result := NewRecorder(store, fixedClock{now: now}).RecordAndAnalyze(RecordInput{
		Namespace:       "default",
		Name:            "web",
		HealthScore:     80,
		HealthState:     "OK",
		DesiredReplicas: 3,
		CurrentReplicas: 2,
		Since:           time.Hour,
		Retention:       24 * time.Hour,
	})
	if !store.appended.Timestamp.Equal(now) || !store.loadNow.Equal(now) || !store.pruneNow.Equal(now) {
		t.Fatalf("clock was not shared: append=%s load=%s prune=%s", store.appended.Timestamp, store.loadNow, store.pruneNow)
	}
	if result.Trend == nil {
		t.Fatal("RecordAndAnalyze() did not return a trend")
	}
}

func TestRecorderSurfacesLoadWarningWithoutDroppingValidSnapshots(t *testing.T) {
	store := &fakeSnapshotStore{loadErr: errors.New("corrupt line")}
	result := NewRecorder(store, fixedClock{now: time.Now()}).RecordAndAnalyze(RecordInput{
		Namespace: "default",
		Name:      "web",
		Since:     time.Hour,
		Retention: time.Hour,
	})
	if result.Trend == nil {
		t.Fatal("valid snapshots should still be analyzed")
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}
