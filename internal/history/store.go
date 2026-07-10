// Package history provides file-based storage for HPA health score
// snapshots using JSONL (JSON Lines) format. Each HPA gets its own
// file named <namespace>_<name>.jsonl in the store directory.
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

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return nil, fmt.Errorf("creating health store directory: %w", err)
	}
	if err := os.Chmod(dir, storeDirMode); err != nil {
		return nil, fmt.Errorf("securing health store directory: %w", err)
	}
	return &HealthStore{dir: dir}, nil
}

// NewHealthStoreWithDir creates a HealthStore using the given directory.
// Used for testing with t.TempDir().
func NewHealthStoreWithDir(dir string) (*HealthStore, error) {
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return nil, fmt.Errorf("creating health store directory: %w", err)
	}
	if err := os.Chmod(dir, storeDirMode); err != nil {
		return nil, fmt.Errorf("securing health store directory: %w", err)
	}
	return &HealthStore{dir: dir}, nil
}

// Append records a health snapshot for the given HPA.
func (s *HealthStore) Append(namespace, name string, snapshot hpa.HealthSnapshot) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name must not be empty")
	}

	path := s.filePath(namespace, name)
	release, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer release()

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
func (s *HealthStore) Load(namespace, name string, since time.Duration) ([]hpa.HealthSnapshot, error) {
	path := s.filePath(namespace, name)
	release, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	defer release()
	return loadHistoryFile(path, since)
}

func loadHistoryFile(path string, since time.Duration) ([]hpa.HealthSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening health store file: %w", err)
	}
	defer func() { _ = f.Close() }()

	cutoff := time.Now().Add(-since)
	var snapshots []hpa.HealthSnapshot

	scanner := bufio.NewScanner(f)
	// Raise the per-line limit to 1MB so that large snapshot lines (big
	// recommendation lists, long diagnosis payloads, etc.) do not trip
	// bufio.ErrTooLong. The default 64KB cap is kept as the initial buffer.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNum := 0
	var corruptLines []int
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var snap hpa.HealthSnapshot
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			corruptLines = append(corruptLines, lineNum)
			continue
		}

		if snap.Timestamp.After(cutoff) {
			snapshots = append(snapshots, snap)
		}
	}

	if err := scanner.Err(); err != nil {
		return snapshots, fmt.Errorf("reading health store file at line %d: %w", lineNum, err)
	}

	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp.Before(snapshots[j].Timestamp)
	})
	if len(corruptLines) > 0 {
		return snapshots, &CorruptLinesError{Path: path, Lines: corruptLines}
	}
	return snapshots, nil
}

// LoadMultiple loads health snapshots for multiple HPAs in batch.
// Returns a map keyed by "namespace/name".
func (s *HealthStore) LoadMultiple(keys []struct{ NS, Name string }, since time.Duration) (map[string][]hpa.HealthSnapshot, error) {
	result := make(map[string][]hpa.HealthSnapshot)
	for _, k := range keys {
		snapshots, err := s.Load(k.NS, k.Name, since)
		if err != nil {
			return nil, fmt.Errorf("loading history for %s/%s: %w", k.NS, k.Name, err)
		}
		if len(snapshots) > 0 {
			result[k.NS+"/"+k.Name] = snapshots
		}
	}
	return result, nil
}

// Prune removes entries older than the retention period from the HPA's file.
func (s *HealthStore) Prune(namespace, name string, retention time.Duration) error {
	path := s.filePath(namespace, name)
	release, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer release()

	snapshots, loadErr := loadHistoryFile(path, retention)
	var corruptErr *CorruptLinesError
	if loadErr != nil {
		if typed, ok := loadErr.(*CorruptLinesError); ok {
			corruptErr = typed
		} else {
			return loadErr
		}
	}

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
	if corruptErr != nil {
		return corruptErr
	}
	return nil
}

// acquireLock uses a portable O_EXCL lock file so separate kubectl processes
// cannot append while another process is pruning the same HPA history.
func acquireLock(path string) (func(), error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
		if err == nil {
			_, _ = fmt.Fprintf(lock, "%d\n", os.Getpid())
			_ = lock.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("creating history lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockTimeout*5 {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for history lock %s", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Dir returns the store directory path.
func (s *HealthStore) Dir() string {
	return s.dir
}

func (s *HealthStore) filePath(namespace, name string) string {
	// Sanitize to prevent path traversal.
	safeNS := sanitizeFilename(namespace)
	safeName := sanitizeFilename(name)
	return filepath.Join(s.dir, safeNS+"_"+safeName+".jsonl")
}

// sanitizeFilename replaces path separators and special characters.
func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	if strings.HasPrefix(s, "..") {
		s = "_" + s
	}
	return s
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
