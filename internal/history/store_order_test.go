package history

import (
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

func TestRecordAndLoadStableInsertion(t *testing.T) {
	for _, offset := range []time.Duration{-3 * time.Minute, -time.Minute, 0, time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			store, err := NewHealthStoreWithDir(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			key := testKey("prod", "default", "web")
			now := time.Now()
			// Deliberately unsorted file with equal timestamps exercises legacy files
			// and wall-clock regressions as well as ordinary chronological appends.
			for i, age := range []time.Duration{0, -2 * time.Minute, -time.Minute, -time.Minute} {
				if err := store.Append(key, healthtrend.HealthSnapshot{Timestamp: now.Add(age), HealthScore: i}); err != nil {
					t.Fatal(err)
				}
			}
			result, err := store.RecordAndLoad(key, healthtrend.HealthSnapshot{Timestamp: now.Add(offset), HealthScore: 99}, time.Hour, time.Hour, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 5 {
				t.Fatalf("got %d records", len(result))
			}
			for i := 1; i < len(result); i++ {
				if result[i].Timestamp.Before(result[i-1].Timestamp) {
					t.Fatalf("unsorted result: %+v", result)
				}
				if result[i].Timestamp.Equal(result[i-1].Timestamp) && result[i].HealthScore < result[i-1].HealthScore {
					t.Fatal("equal timestamp order changed")
				}
			}
			loaded, err := store.LoadAt(key, time.Hour, now)
			if err != nil {
				t.Fatal(err)
			}
			for i := range loaded {
				if !loaded[i].Timestamp.Equal(result[i].Timestamp) || loaded[i].HealthScore != result[i].HealthScore {
					t.Fatalf("disk differs at %d", i)
				}
			}
		})
	}
}
