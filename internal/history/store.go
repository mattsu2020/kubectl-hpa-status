// Package history provides file-based storage for HPA health score
// snapshots using JSONL (JSON Lines) format. Each HPA gets its own
// file named <namespace>_<name>.jsonl in the store directory.
package history

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	storeDirMode   = 0o700
	storeFileMode  = 0o600
	lockTimeout    = 2 * time.Second
	lockStaleAfter = 30 * time.Second
	lockHeartbeat  = 5 * time.Second
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
func (s *HealthStore) Append(namespace, name string, snapshot healthtrend.HealthSnapshot) error {
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
func (s *HealthStore) Load(namespace, name string, since time.Duration) ([]healthtrend.HealthSnapshot, error) {
	return s.LoadAt(namespace, name, since, time.Now())
}

// LoadAt reads health snapshots relative to the supplied time. Application
// services use this form so one command run has a consistent, testable clock.
func (s *HealthStore) LoadAt(namespace, name string, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	path := s.filePath(namespace, name)
	release, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	defer release()
	return loadHistoryFileAt(path, since, now)
}

func loadHistoryFileAt(path string, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening health store file: %w", err)
	}
	defer func() { _ = f.Close() }()

	cutoff := now.Add(-since)
	var snapshots []healthtrend.HealthSnapshot

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

		var snap healthtrend.HealthSnapshot
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
func (s *HealthStore) LoadMultiple(keys []struct{ NS, Name string }, since time.Duration) (map[string][]healthtrend.HealthSnapshot, error) {
	result := make(map[string][]healthtrend.HealthSnapshot)
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
	return s.PruneAt(namespace, name, retention, time.Now())
}

// PruneAt removes entries older than retention relative to the supplied time.
func (s *HealthStore) PruneAt(namespace, name string, retention time.Duration, now time.Time) error {
	path := s.filePath(namespace, name)
	release, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer release()

	snapshots, loadErr := loadHistoryFileAt(path, retention, now)
	var corruptErr *CorruptLinesError
	if loadErr != nil && !errors.As(loadErr, &corruptErr) {
		return loadErr
	}

	if err := s.replaceSnapshots(path, snapshots); err != nil {
		return err
	}
	if corruptErr != nil {
		return corruptErr
	}
	return nil
}

// RecordAndLoad atomically appends a snapshot, applies retention, and returns
// the requested analysis window while holding one inter-process lock.
func (s *HealthStore) RecordAndLoad(namespace, name string, snapshot healthtrend.HealthSnapshot, retention, since time.Duration, now time.Time) ([]healthtrend.HealthSnapshot, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and name must not be empty")
	}
	path := s.filePath(namespace, name)
	release, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	defer release()

	snapshots, loadErr := loadHistoryFileAt(path, retention, now)
	var corruptErr *CorruptLinesError
	if loadErr != nil && !errors.As(loadErr, &corruptErr) {
		return nil, loadErr
	}
	snapshots = append(snapshots, snapshot)
	sort.SliceStable(snapshots, func(i, j int) bool { return snapshots[i].Timestamp.Before(snapshots[j].Timestamp) })
	if err := s.replaceSnapshots(path, snapshots); err != nil {
		return nil, err
	}
	cutoff := now.Add(-since)
	start := sort.Search(len(snapshots), func(i int) bool { return !snapshots[i].Timestamp.Before(cutoff) })
	window := append([]healthtrend.HealthSnapshot(nil), snapshots[start:]...)
	if corruptErr != nil {
		return window, corruptErr
	}
	return window, nil
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

// acquireLock uses a portable O_EXCL lock file so separate kubectl processes
// cannot append while another process is pruning the same HPA history.
func acquireLock(path string) (func(), error) {
	return acquireLockContext(context.Background(), path)
}

// acquireLockContext acquires an ownership-aware lock. A heartbeat prevents a
// long-running live operation from being mistaken for an abandoned lock.
func acquireLockContext(ctx context.Context, path string) (func(), error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("creating history lock token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	for {
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
		if err == nil {
			if _, writeErr := fmt.Fprintf(lock, "%s %d\n", token, os.Getpid()); writeErr != nil {
				_ = lock.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("writing history lock: %w", writeErr)
			}
			if closeErr := lock.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("closing history lock: %w", closeErr)
			}
			stop := make(chan struct{})
			done := make(chan struct{})
			go heartbeatLock(lockPath, token, stop, done)
			return func() {
				close(stop)
				<-done
				removeLockIfOwned(lockPath, token)
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("creating history lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			quarantine := fmt.Sprintf("%s.stale-%s", lockPath, token)
			if renameErr := os.Rename(lockPath, quarantine); renameErr == nil {
				_ = os.Remove(quarantine)
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for history lock %s", lockPath)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for history lock %s: %w", lockPath, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func heartbeatLock(path, token string, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(lockHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			if !lockOwnedBy(path, token) {
				return
			}
			_ = os.Chtimes(path, now, now)
		}
	}
}

func removeLockIfOwned(path, token string) {
	if lockOwnedBy(path, token) {
		_ = os.Remove(path)
	}
}

func lockOwnedBy(path, token string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- internally derived lock path
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 0 && fields[0] == token
}

// Dir returns the store directory path.
func (s *HealthStore) Dir() string {
	return s.dir
}

func (s *HealthStore) filePath(namespace, name string) string {
	// Sanitize to prevent path traversal.
	safeNS := sanitizeFilename(namespace)
	safeName := sanitizeFilename(name)
	filename := safeNS + "_" + safeName + ".jsonl"
	// sanitizeFilename truncates individual components. Add an identity hash
	// whenever that happens, even if the combined filename still fits, so two
	// names with the same prefix never share a history stream.
	truncated := len(namespace) > maxFilenameSegmentLength || len(name) > maxFilenameSegmentLength
	if truncated || len(filename) > maxHistoryFilenameLength {
		sum := sha256.Sum256([]byte(namespace + "\x00" + name))
		suffix := fmt.Sprintf("_%x.jsonl", sum[:8])
		prefixLength := maxHistoryFilenameLength - len(suffix)
		prefix := safeNS + "_" + safeName
		if len(prefix) > prefixLength {
			prefix = prefix[:prefixLength]
		}
		filename = prefix + suffix
	}
	return filepath.Join(s.dir, filename)
}

// Keep enough headroom below the common NAME_MAX=255 byte limit. A hash is
// appended whenever truncation is required so two long HPA names cannot share
// one history file.
const maxHistoryFilenameLength = 240

// maxFilenameSegmentLength bounds a single sanitized path segment so a
// pathologically long name cannot blow past filesystem name limits.
const maxFilenameSegmentLength = 200

// sanitizeFilename neutralizes anything that could escape the store
// directory: path separators, parent-directory references, control
// characters, empty inputs, and over-long names. Inputs come from the
// Kubernetes API and are DNS-constrained in practice, so this is
// defense-in-depth.
func sanitizeFilename(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	s = strings.ReplaceAll(b.String(), "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	if s == "" || s == "." || strings.HasPrefix(s, "..") {
		s = "_" + s
	}
	if len(s) > maxFilenameSegmentLength {
		s = s[:maxFilenameSegmentLength]
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
