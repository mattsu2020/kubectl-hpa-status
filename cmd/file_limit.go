package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxInputFileSize caps files that are read fully into memory (candidate
	// HPA manifests, recorded JSON traces, gitops manifests, config files).
	// It prevents out-of-memory aborts when a huge or hostile file is passed,
	// e.g. from a CI/CD pipeline. Streaming JSONL readers are bounded by
	// maxSnapshotsPerTrace instead.
	maxInputFileSize = 50 * 1024 * 1024 // 50 MiB

	// maxSnapshotsPerTrace bounds in-memory snapshot accumulation when
	// streaming JSONL record files, preventing unbounded memory growth on
	// pathologically large recordings.
	maxSnapshotsPerTrace = 1_000_000

	// maxWalkDepth limits how deep manifest collection recurses into a
	// directory tree, so a stray `lint /` cannot scan an entire filesystem.
	maxWalkDepth = 20

	// maxWalkFiles limits how many manifest files a single directory walk may
	// collect.
	maxWalkFiles = 10_000
)

// walkSkipDirs are directory names that never contain user manifests but can
// hold millions of files (VCS metadata, dependency caches).
var walkSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// readFileBounded reads path into memory, refusing files larger than
// maxInputFileSize. Callers wrap the returned error with context.
func readFileBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxInputFileSize {
		return nil, fmt.Errorf(
			"file %s is %d bytes, exceeding the %d MiB input limit",
			path, info.Size(), maxInputFileSize/(1024*1024),
		)
	}
	return os.ReadFile(path) // #nosec G304 -- path comes from an explicit user flag, and size is bounded above
}

// snapshotLimitError builds the error returned when a record file accumulates
// more snapshots than maxSnapshotsPerTrace.
func snapshotLimitError(path string) error {
	return fmt.Errorf(
		"record file %s exceeds %d snapshots per HPA; split the recording or reduce retention",
		path, maxSnapshotsPerTrace,
	)
}

// collectManifestFiles walks root and returns every YAML/JSON file beneath it,
// bounded by maxWalkDepth and maxWalkFiles and skipping VCS/dependency
// directories. Shared by the lint and gitops review collectors, which used to
// carry identical unbounded filepath.Walk implementations.
func collectManifestFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			if path != root && walkSkipDirs[fi.Name()] {
				return filepath.SkipDir
			}
			if walkDepth(root, path) > maxWalkDepth {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		if len(files) >= maxWalkFiles {
			return fmt.Errorf("directory walk exceeded %d files; narrow the target path", maxWalkFiles)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}
	return files, nil
}

// walkDepth counts the directory levels between root and path (root itself is
// depth 0, a direct child is depth 1).
func walkDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}
