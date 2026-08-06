package util

import (
	"fmt"
	"os"
)

// MaxInputFileSize caps files that are read fully into memory by pkg-level
// loaders (recorded JSON traces, policy files). It prevents out-of-memory
// aborts when a huge or hostile file is passed. Keep in sync with the
// cmd-level maxInputFileSize in cmd/file_limit.go: Go's internal-package
// visibility rules prevent cmd from importing this constant, so the two
// limits are duplicated deliberately.
const MaxInputFileSize = 50 * 1024 * 1024 // 50 MiB

// ReadFileBounded reads path into memory, refusing files larger than
// MaxInputFileSize. Callers wrap the returned error with context.
func ReadFileBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxInputFileSize {
		return nil, fmt.Errorf(
			"file %s is %d bytes, exceeding the %d MiB input limit",
			path, info.Size(), MaxInputFileSize/(1024*1024),
		)
	}
	return os.ReadFile(path) // #nosec G304 -- path comes from an explicit user flag, and size is bounded above
}
