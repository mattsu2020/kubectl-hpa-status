package history

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

func TestHistoryCachePreservesWindowsAndExternalUpdates(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := testKey("prod", "default", "web")
	now := time.Now().Round(0)
	for i, age := range []time.Duration{-2 * time.Hour, -time.Hour, 0} {
		if err = store.Append(t.Context(), key, healthtrend.HealthSnapshot{Timestamp: now.Add(age), HealthScore: i}); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		now   time.Time
		since time.Duration
		want  int
	}{{now, 3 * time.Hour, 3}, {now, 30 * time.Minute, 1}, {now, 3 * time.Hour, 3}, {now.Add(-time.Hour), 3 * time.Hour, 3}} {
		records, e := store.LoadAt(t.Context(), key, test.since, test.now)
		if e != nil || len(records) != test.want {
			t.Fatalf("window %+v: %v %v", test, records, e)
		}
		records[0].HealthScore = 999
	}
	// A second store instance appends to the same stream. Its data must invalidate
	// the first instance's decoded observation.
	other, err := NewHealthStoreWithDir(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err = other.Append(t.Context(), key, healthtrend.HealthSnapshot{Timestamp: now, HealthScore: 42}); err != nil {
		t.Fatal(err)
	}
	records, err := store.LoadAt(t.Context(), key, 3*time.Hour, now)
	if err != nil || len(records) != 4 || records[0].HealthScore != 0 || records[3].HealthScore != 42 {
		t.Fatalf("external append or ownership: %v %v", records, err)
	}
	// Replacement with the same byte size and mtime must still invalidate via identity.
	path := store.filePath(key)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(store.Dir(), "replacement.jsonl")
	if err = os.WriteFile(replacement, bytes.Replace(data, []byte(`"healthScore":42`), []byte(`"healthScore":43`), 1), storeFileMode); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	records, err = store.LoadAt(t.Context(), key, 3*time.Hour, now)
	if err != nil || len(records) != 4 || records[3].HealthScore != 43 {
		t.Fatalf("replaced history: %v %v", records, err)
	}
	// A truncate/overwrite by a non-store writer is also detected.
	if err = os.WriteFile(path, []byte("broken\n"), storeFileMode); err != nil {
		t.Fatal(err)
	}
	records, err = store.LoadAt(t.Context(), key, 3*time.Hour, now)
	var corrupt *CorruptLinesError
	if len(records) != 0 || !errors.As(err, &corrupt) {
		t.Fatalf("external corruption hidden: %v %v", records, err)
	}
}

func TestCachedRecordAndLoadMatchesDiskAcrossRetention(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := testKey("prod", "default", "web")
	now := time.Now().Round(0)
	for i := 0; i < 150; i++ {
		clock := now.Add(time.Duration(i) * time.Minute)
		if i == 100 {
			clock = clock.Add(-10 * time.Minute)
		}
		records, e := store.RecordAndLoad(t.Context(), key, healthtrend.HealthSnapshot{Timestamp: clock, HealthScore: i}, 30*time.Minute, 20*time.Minute, clock)
		if e != nil {
			t.Fatal(e)
		}
		disk, e := loadHistoryFileAtUncached(store.filePath(key), 20*time.Minute, clock)
		if e != nil {
			t.Fatal(e)
		}
		// Preserve the historical inclusive analysis cutoff in RecordAndLoad; disk
		// readers use a strict cutoff. Compare records strictly inside the window.
		fresh := records[:0]
		for _, record := range records {
			if record.Timestamp.After(clock.Add(-20 * time.Minute)) {
				fresh = append(fresh, record)
			}
		}
		if len(fresh) != len(disk) {
			t.Fatalf("iteration %d: cache=%d disk=%d", i, len(fresh), len(disk))
		}
		for j := range disk {
			if fresh[j] != disk[j] {
				t.Fatalf("iteration %d record %d: cache=%+v disk=%+v", i, j, fresh[j], disk[j])
			}
		}
	}
}

func loadHistoryFileAtUncached(path string, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	scan, err := scanHistoryFile(path, since, now)
	return scan.retained, err
}

func TestScanCacheBounds(t *testing.T) {
	cache := &scanCache{entries: make(map[string]cachedScan)}
	dir := t.TempDir()
	now := time.Now().Round(0)
	for i := 0; i < maxCachedHistoryStreams+1; i++ {
		path := filepath.Join(dir, time.Unix(int64(i), 0).Format("150405")+".jsonl")
		if err := os.WriteFile(path, nil, storeFileMode); err != nil {
			t.Fatal(err)
		}
		cache.save(path, now, historyScan{retained: []healthtrend.HealthSnapshot{{Timestamp: now}}})
	}
	if len(cache.entries) != maxCachedHistoryStreams || cache.records != maxCachedHistoryStreams {
		t.Fatalf("unbounded cache: streams=%d records=%d", len(cache.entries), cache.records)
	}
	path := filepath.Join(dir, "oversized.jsonl")
	if err := os.WriteFile(path, nil, storeFileMode); err != nil {
		t.Fatal(err)
	}
	cache.save(path, now, historyScan{retained: make([]healthtrend.HealthSnapshot, maxCachedHistoryRecords+1)})
	if _, ok := cache.load(path, now); ok {
		t.Fatal("oversized history was cached")
	}
}

func TestEmptyHistoryDirectoryIsRejected(t *testing.T) {
	if _, err := NewHealthStoreWithDir(""); err == nil {
		t.Fatal("empty path must not resolve to the working directory")
	}
}
