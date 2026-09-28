package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// BenchmarkRecordAndLoad measures fixed-size histories, including their O(n)
// read cost. Fixture restoration is excluded from both time and allocations.
func BenchmarkRecordAndLoad(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("retained_%d", count), func(b *testing.B) { benchmarkRecordAndLoad(b, count, false) })
	}
}

// BenchmarkRecordAndLoadCompaction restores expired records before EVERY
// iteration, so every measured operation performs compaction.
func BenchmarkRecordAndLoadCompaction(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("expired_%d", count), func(b *testing.B) { benchmarkRecordAndLoad(b, count, true) })
	}
}

func benchmarkRecordAndLoad(b *testing.B, count int, expired bool) {
	store, err := NewHealthStoreWithDir(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	key := testKey("bench", "default", "web")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var fixture bytes.Buffer
	encoder := json.NewEncoder(&fixture)
	for i := 0; i < count; i++ {
		timestamp := now.Add(-time.Duration(count-i) * time.Second)
		if expired {
			timestamp = timestamp.Add(-48 * time.Hour)
		}
		if err := encoder.Encode(healthtrend.HealthSnapshot{Timestamp: timestamp, HealthScore: 90, HealthState: "OK"}); err != nil {
			b.Fatal(err)
		}
	}
	snapshot := healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 90, HealthState: "OK"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := os.WriteFile(store.filePath(key), fixture.Bytes(), storeFileMode); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		window, err := store.RecordAndLoad(key, snapshot, 24*time.Hour, 24*time.Hour, now)
		b.StopTimer()
		if err != nil {
			b.Fatal(err)
		}
		want := count + 1
		if expired {
			want = 1
		}
		if len(window) != want {
			b.Fatalf("window size = %d, want %d", len(window), want)
		}
		if expired {
			data, err := os.ReadFile(store.filePath(key))
			if err != nil {
				b.Fatal(err)
			}
			if bytes.Count(data, []byte("\n")) != 1 {
				b.Fatal("expired records were not compacted")
			}
		}
	}
}
