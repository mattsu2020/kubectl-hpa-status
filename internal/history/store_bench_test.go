package history

import (
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// BenchmarkRecordAndLoad measures the steady-state cost of one health record
// on a history holding thousands of retained snapshots — the shape of a
// long-running, short-interval watch. The append-only record path keeps the
// per-record cost flat as history grows; the previous implementation paid a
// full read-sort-rewrite of the retained window on every observation.
func BenchmarkRecordAndLoad(b *testing.B) {
	store, err := NewHealthStoreWithDir(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	key := testKey("bench", "default", "web")
	now := time.Now()

	seed := healthtrend.HealthSnapshot{HealthScore: 90, HealthState: "OK"}
	for i := 0; i < 5000; i++ {
		seed.Timestamp = now.Add(-time.Duration(i) * time.Second)
		if err := appendSnapshotLine(store.filePath(key), seed); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seed.Timestamp = now.Add(time.Duration(i+1) * time.Second)
		if _, err := store.RecordAndLoad(key, seed, 24*time.Hour, time.Hour, seed.Timestamp); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRecordAndLoadCompaction measures the record cost once compaction
// runs every time: the worst case, where most of the history has expired.
func BenchmarkRecordAndLoadCompaction(b *testing.B) {
	store, err := NewHealthStoreWithDir(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	key := testKey("bench", "default", "web")
	now := time.Now()

	seed := healthtrend.HealthSnapshot{HealthScore: 90, HealthState: "OK"}
	for i := 0; i < 5000; i++ {
		// All lines sit beyond the retention cutoff, so every record triggers
		// the rewrite path.
		seed.Timestamp = now.Add(-48*time.Hour - time.Duration(i)*time.Second)
		if err := appendSnapshotLine(store.filePath(key), seed); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seed.Timestamp = now.Add(time.Duration(i+1) * time.Second)
		if _, err := store.RecordAndLoad(key, seed, 24*time.Hour, time.Hour, seed.Timestamp); err != nil {
			b.Fatal(err)
		}
	}
}
