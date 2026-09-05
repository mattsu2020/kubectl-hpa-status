// Package history provides file-based storage for HPA health score
// snapshots using JSONL (JSON Lines) format. Each HPA history stream is
// keyed by cluster identity, namespace, name, and the HPA object UID, and
// stored as <cluster>_<namespace>_<name>_<identity-hash>.jsonl in the store
// directory.
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

// HealthStore manages file-based persistence of health snapshots.
// It stores one JSONL file per HPA in the configured directory.
type HealthStore struct {
	dir string
}

const (
	storeDirMode  = 0o700
	storeFileMode = 0o600
	lockTimeout   = 2 * time.Second

	// compactionMinExpired is the absolute number of expired lines at which a
	// RecordAndLoad rewrites the file instead of appending. Below both
	// thresholds expired lines simply stay on disk — every reader filters by
	// its own cutoff — so steady-state recording stays a single O(1) append
	// instead of rewriting the whole history on every observation.
	compactionMinExpired = 64
)

// CorruptLinesError reports malformed JSONL records while valid snapshots are
// still returned to the caller.
type CorruptLinesError struct {
	Path  string
	Lines []int
}

func (e *CorruptLinesError) Error() string {
	return fmt.Sprintf("history file %s contains %d corrupt line(s): %v", e.Path, len(e.Lines), e.Lines)
}

// NewHealthStore creates a HealthStore using the platform cache directory.
// Falls back to ~/.kubectl-hpa-status/history/ if XDG_CACHE_HOME is not set.
func NewHealthStore() (*HealthStore, error) {
	dir, err := resolveStoreDir()
	if err != nil {
		return nil, fmt.Errorf("resolving health store directory: %w", err)
	}
	return newHealthStoreAt(dir)
}

// NewHealthStoreWithDir creates a HealthStore using the given directory.
// Used for testing with t.TempDir().
func NewHealthStoreWithDir(dir string) (*HealthStore, error) {
	return newHealthStoreAt(dir)
}

func newHealthStoreAt(dir string) (*HealthStore, error) {
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return nil, fmt.Errorf("creating health store directory: %w", err)
	}
	if err := os.Chmod(dir, storeDirMode); err != nil {
		return nil, fmt.Errorf("securing health store directory: %w", err)
	}
	return &HealthStore{dir: dir}, nil
}

// Append records a health snapshot for the given HPA.
func (s *HealthStore) Append(key SnapshotKey, snapshot healthtrend.HealthSnapshot) error {
	if err := key.validate(); err != nil {
		return err
	}

	path := s.filePath(key)
	release, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer release()

	return appendSnapshotLine(path, snapshot)
}

func appendSnapshotLine(path string, snapshot healthtrend.HealthSnapshot) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, storeFileMode)
	if err != nil {
		return fmt.Errorf("opening health store file: %w", err)
	}
	if err := f.Chmod(storeFileMode); err != nil {
		_ = f.Close()
		return fmt.Errorf("securing health store file: %w", err)
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("marshaling health snapshot: %w", err)
	}

	if _, err = fmt.Fprintln(f, string(data)); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing health snapshot: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("syncing health snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing health snapshot: %w", err)
	}
	return nil
}

// Load reads health snapshots for the given HPA within the specified time window.
// Returns snapshots sorted by timestamp (oldest first).
func (s *HealthStore) Load(key SnapshotKey, since time.Duration) ([]healthtrend.HealthSnapshot, error) {
	return s.LoadAt(key, since, time.Now())
}

// LoadAt reads health snapshots relative to the supplied time. Application
// services use this form so one command run has a consistent, testable clock.
func (s *HealthStore) LoadAt(key SnapshotKey, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	path := s.filePath(key)
	release, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	defer release()
	return loadHistoryFileAt(path, since, now)
}

// LoadMultiple loads health snapshots for multiple HPAs in batch.
// Returns a map keyed by SnapshotKey.String().
func (s *HealthStore) LoadMultiple(keys []SnapshotKey, since time.Duration) (map[string][]healthtrend.HealthSnapshot, error) {
	result := make(map[string][]healthtrend.HealthSnapshot)
	for _, key := range keys {
		snapshots, err := s.Load(key, since)
		if err != nil {
			return nil, fmt.Errorf("loading history for %s: %w", key, err)
		}
		if len(snapshots) > 0 {
			result[key.String()] = snapshots
		}
	}
	return result, nil
}

// Prune removes entries older than the retention period from the HPA's file.
func (s *HealthStore) Prune(key SnapshotKey, retention time.Duration) error {
	return s.PruneAt(key, retention, time.Now())
}

// PruneAt removes entries older than retention relative to the supplied time.
func (s *HealthStore) PruneAt(key SnapshotKey, retention time.Duration, now time.Time) error {
	path := s.filePath(key)
	release, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer release()

	scan, loadErr := scanHistoryFile(path, retention, now)
	if loadErr != nil {
		return loadErr
	}

	if err := s.replaceSnapshots(path, scan.retained); err != nil {
		return err
	}
	if len(scan.corruptLines) > 0 {
		return &CorruptLinesError{Path: path, Lines: scan.corruptLines}
	}
	return nil
}

// RecordAndLoad atomically appends a snapshot and returns the requested
// analysis window while holding one inter-process lock. The append is a
// single O(1) write; the whole-file rewrite is deferred to a compaction pass
// that only runs once enough history has expired (see shouldCompact), so the
// cost of a record no longer grows with the size of the retained history.
func (s *HealthStore) RecordAndLoad(key SnapshotKey, snapshot healthtrend.HealthSnapshot, retention, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	path := s.filePath(key)
	release, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	defer release()

	scan, err := scanHistoryFile(path, retention, now)
	if err != nil {
		return nil, err
	}
	retained := append(scan.retained, snapshot)
	sort.SliceStable(retained, func(i, j int) bool { return retained[i].Timestamp.Before(retained[j].Timestamp) })

	if shouldCompact(scan.total, scan.expired) {
		if err := s.replaceSnapshots(path, retained); err != nil {
			return nil, err
		}
	} else {
		if err := appendSnapshotLine(path, snapshot); err != nil {
			return nil, err
		}
	}

	cutoff := now.Add(-since)
	start := sort.Search(len(retained), func(i int) bool { return !retained[i].Timestamp.Before(cutoff) })
	window := append([]healthtrend.HealthSnapshot(nil), retained[start:]...)
	if len(scan.corruptLines) > 0 {
		return window, &CorruptLinesError{Path: path, Lines: scan.corruptLines}
	}
	return window, nil
}

// shouldCompact decides whether the history file is rewritten during a
// record. Expired entries are harmless to leave behind — every reader filters
// by its own time cutoff — so compaction waits until they are a meaningful
// share of the file (or an absolute count) before paying for a rewrite.
func shouldCompact(total, expired int) bool {
	if expired <= 0 {
		return false
	}
	return expired >= compactionMinExpired || expired*2 >= total
}

// historyScan is the result of classifying every valid line of a history
// file against a retention cutoff.
type historyScan struct {
	// total counts every decodable, non-empty line.
	total int
	// expired counts lines at or beyond the retention cutoff. Their payloads
	// are dropped at scan time; only the count is kept.
	expired int
	// retained holds the still-fresh snapshots in file order.
	retained []healthtrend.HealthSnapshot
	// corruptLines records the line numbers of undecodable lines.
	corruptLines []int
}

// scanHistoryFile classifies the lines of one history file. A missing file is
// an empty scan, mirroring the previous load behaviour.
func scanHistoryFile(path string, retention time.Duration, now time.Time) (historyScan, error) {
	scan := historyScan{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return scan, nil
		}
		return scan, fmt.Errorf("opening health store file: %w", err)
	}
	defer func() { _ = f.Close() }()

	cutoff := now.Add(-retention)

	scanner := bufio.NewScanner(f)
	// Raise the per-line limit to 1MB so that large snapshot lines (big
	// recommendation lists, long diagnosis payloads, etc.) do not trip
	// bufio.ErrTooLong. The default 64KB cap is kept as the initial buffer.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var snap healthtrend.HealthSnapshot
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			scan.corruptLines = append(scan.corruptLines, lineNum)
			continue
		}

		scan.total++
		if snap.Timestamp.After(cutoff) {
			scan.retained = append(scan.retained, snap)
		} else {
			scan.expired++
		}
	}
	if err := scanner.Err(); err != nil {
		return scan, fmt.Errorf("reading health store file at line %d: %w", lineNum, err)
	}
	sort.SliceStable(scan.retained, func(i, j int) bool { return scan.retained[i].Timestamp.Before(scan.retained[j].Timestamp) })
	return scan, nil
}

func (s *HealthStore) replaceSnapshots(path string, snapshots []healthtrend.HealthSnapshot) error {
	tmp, err := os.CreateTemp(s.dir, ".history-*.jsonl")
	if err != nil {
		return fmt.Errorf("creating temporary health store file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(storeFileMode); err != nil {
		return fmt.Errorf("securing temporary health store file: %w", err)
	}

	writer := bufio.NewWriter(tmp)
	for _, snap := range snapshots {
		data, err := json.Marshal(snap)
		if err != nil {
			return fmt.Errorf("marshaling retained health snapshot: %w", err)
		}
		if _, err := fmt.Fprintln(writer, string(data)); err != nil {
			return fmt.Errorf("writing retained health snapshot: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flushing health store file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("syncing health store file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing health store file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing health store file: %w", err)
	}
	return nil
}

// Dir returns the store directory path.
func (s *HealthStore) Dir() string {
	return s.dir
}

// resolveStoreDir returns the directory for health history storage.
func resolveStoreDir() (string, error) {
	// Check XDG_CACHE_HOME first.
	xdg := os.Getenv("XDG_CACHE_HOME")
	if xdg != "" {
		return filepath.Join(xdg, "kubectl-hpa-status", "history"), nil
	}

	// Fallback to home directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".kubectl-hpa-status", "history"), nil
}

// loadHistoryFileAt reads the snapshots within the since window, sorted by
// timestamp (oldest first). Undecodable lines are reported through
// CorruptLinesError while the valid prefix is still returned.
func loadHistoryFileAt(path string, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	scan, err := scanHistoryFile(path, since, now)
	if err != nil {
		return scan.retained, err
	}
	if len(scan.corruptLines) > 0 {
		return scan.retained, &CorruptLinesError{Path: path, Lines: scan.corruptLines}
	}
	return scan.retained, nil
}
