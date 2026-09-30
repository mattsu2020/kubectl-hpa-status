package history

import (
	"os"
	"sort"
	"sync"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// The CLI constructs stores for each report, so the cache is process-wide and
// keyed by the full file path. Advisory file locks still guard every operation.
// Both the stream count and total decoded records are bounded; eviction changes
// performance only. File identity, size, and modification time invalidate data
// changed by another store/process, and a wider window forces a fresh scan.
const (
	maxCachedHistoryStreams = 128
	maxCachedHistoryRecords = 100000
)

type cachedScan struct {
	info   os.FileInfo
	cutoff time.Time
	scan   historyScan
	used   uint64
}

type scanCache struct {
	mu       sync.Mutex
	entries  map[string]cachedScan
	records  int
	sequence uint64
}

var sharedScanCache = &scanCache{entries: make(map[string]cachedScan)}

func cachedHistoryScan(path string, retention time.Duration, now time.Time) (historyScan, error) {
	cutoff := now.Add(-retention)
	if scan, ok := sharedScanCache.load(path, cutoff); ok {
		return scan, nil
	}
	scan, err := scanHistoryFile(path, retention, now)
	if err == nil {
		sharedScanCache.save(path, cutoff, scan)
	}
	return scan, err
}

func (c *scanCache) load(path string, cutoff time.Time) (historyScan, bool) {
	info, err := os.Stat(path)
	if err != nil {
		c.remove(path)
		return historyScan{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.entries[path]
	if !ok {
		return historyScan{}, false
	}
	if cutoff.Before(cached.cutoff) || !os.SameFile(info, cached.info) || info.Size() != cached.info.Size() || !info.ModTime().Equal(cached.info.ModTime()) {
		c.removeLocked(path)
		return historyScan{}, false
	}
	c.sequence++
	cached.used = c.sequence
	c.entries[path] = cached
	scan := cached.scan
	start := sort.Search(len(scan.retained), func(i int) bool { return scan.retained[i].Timestamp.After(cutoff) })
	scan.expired += start
	scan.retained = append([]healthtrend.HealthSnapshot(nil), scan.retained[start:]...)
	scan.corruptLines = append([]int(nil), scan.corruptLines...)
	return scan, true
}

func (c *scanCache) save(path string, cutoff time.Time, scan historyScan) {
	info, err := os.Stat(path)
	if err != nil {
		c.remove(path)
		return
	}
	count := len(scan.retained) + len(scan.corruptLines)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeLocked(path)
	if count > maxCachedHistoryRecords {
		return
	}
	for len(c.entries) >= maxCachedHistoryStreams || c.records+count > maxCachedHistoryRecords {
		var oldest string
		var used uint64
		for key, entry := range c.entries {
			if oldest == "" || entry.used < used {
				oldest, used = key, entry.used
			}
		}
		c.removeLocked(oldest)
	}
	c.sequence++
	scan.retained = append([]healthtrend.HealthSnapshot(nil), scan.retained...)
	scan.corruptLines = append([]int(nil), scan.corruptLines...)
	c.entries[path] = cachedScan{info: info, cutoff: cutoff, scan: scan, used: c.sequence}
	c.records += count
}

func (c *scanCache) remove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeLocked(path)
}

func (c *scanCache) removeLocked(path string) {
	if entry, ok := c.entries[path]; ok {
		c.records -= len(entry.scan.retained) + len(entry.scan.corruptLines)
		delete(c.entries, path)
	}
}
