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
	appended   healthtrend.HealthSnapshot
	appendedTo SnapshotKey
	loadNow    time.Time
	pruneNow   time.Time
	loadErr    error
}

func (s *fakeSnapshotStore) Append(key SnapshotKey, snapshot healthtrend.HealthSnapshot) error {
	s.appendedTo = key
	s.appended = snapshot
	return nil
}

func (s *fakeSnapshotStore) LoadAt(_ SnapshotKey, _ time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	s.loadNow = now
	return []healthtrend.HealthSnapshot{s.appended}, s.loadErr
}

func (s *fakeSnapshotStore) PruneAt(_ SnapshotKey, _ time.Duration, now time.Time) error {
	s.pruneNow = now
	return nil
}

func TestRecorderUsesOneClockAndReturnsTrend(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	store := &fakeSnapshotStore{}
	result := NewRecorder(store, fixedClock{now: now}).RecordAndAnalyze(RecordInput{
		Cluster:         "prod",
		UID:             "uid-1",
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
	want := (SnapshotKey{Cluster: "prod", Namespace: "default", Name: "web", UID: "uid-1"}).String()
	if store.appendedTo.String() != want {
		t.Fatalf("store received key %q, want %q", store.appendedTo, want)
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

func TestRecorderUsesHealthStoreTransaction(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result := NewRecorder(store, fixedClock{now: now}).RecordAndAnalyze(RecordInput{
		Cluster: "prod", UID: "uid-1",
		Namespace: "default", Name: "web", HealthScore: 90, HealthState: "OK",
		Since: time.Hour, Retention: 24 * time.Hour,
	})
	if result.Trend == nil {
		t.Fatal("transaction did not return a trend")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	snapshots, err := store.LoadAt(SnapshotKey{Cluster: "prod", Namespace: "default", Name: "web", UID: "uid-1"}, time.Hour, now)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("stored snapshots=%d err=%v", len(snapshots), err)
	}
}
