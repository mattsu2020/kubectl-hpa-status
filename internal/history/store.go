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
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// HealthStore manages file-based persistence of health snapshots.
// It stores one JSONL file per HPA in the configured directory.
type HealthStore struct {
	dir string
}

// NewHealthStore creates a HealthStore using the platform cache directory.
// Falls back to ~/.kubectl-hpa-status/history/ if XDG_CACHE_HOME is not set.
func NewHealthStore() (*HealthStore, error) {
	dir, err := resolveStoreDir()
	if err != nil {
		return nil, fmt.Errorf("resolving health store directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating health store directory: %w", err)
	}
	return &HealthStore{dir: dir}, nil
}

// NewHealthStoreWithDir creates a HealthStore using the given directory.
// Used for testing with t.TempDir().
func NewHealthStoreWithDir(dir string) (*HealthStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating health store directory: %w", err)
	}
	return &HealthStore{dir: dir}, nil
}

// Append records a health snapshot for the given HPA.
func (s *HealthStore) Append(namespace, name string, snapshot hpa.HealthSnapshot) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name must not be empty")
	}

	path := s.filePath(namespace, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening health store file: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshaling health snapshot: %w", err)
	}

	_, err = fmt.Fprintln(f, string(data))
	return err
}

// Load reads health snapshots for the given HPA within the specified time window.
// Returns snapshots sorted by timestamp (oldest first).
func (s *HealthStore) Load(namespace, name string, since time.Duration) ([]hpa.HealthSnapshot, error) {
	path := s.filePath(namespace, name)

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
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var snap hpa.HealthSnapshot
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			// Skip corrupt lines rather than failing.
			continue
		}

		if snap.Timestamp.After(cutoff) {
			snapshots = append(snapshots, snap)
		}
	}

	if err := scanner.Err(); err != nil {
		return snapshots, fmt.Errorf("reading health store file at line %d: %w", lineNum, err)
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
	snapshots, err := s.Load(namespace, name, retention)
	if err != nil {
		return err
	}

	// Rewrite the file with only the retained entries.
	path := s.filePath(namespace, name)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("rewriting health store file: %w", err)
	}
	defer func() { _ = f.Close() }()

	writer := bufio.NewWriter(f)
	for _, snap := range snapshots {
		data, err := json.Marshal(snap)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintln(writer, string(data)); err != nil {
			return err
		}
	}
	return writer.Flush()
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
