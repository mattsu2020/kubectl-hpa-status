package util

import "github.com/mattsu2020/kubectl-hpa-status/internal/fileio"

// MaxInputFileSize caps files that are read fully into memory by pkg-level
// loaders (recorded JSON traces, policy files). It prevents out-of-memory
// aborts when a huge or hostile file is passed. Keep in sync with the
// cmd-level maxInputFileSize in cmd/file_limit.go: Go's internal-package
// visibility rules prevent cmd from importing this constant, so the two
// limits are duplicated deliberately.
const MaxInputFileSize = fileio.MaxInputFileSize

// ReadFileBounded reads path into memory, refusing files larger than
// MaxInputFileSize. Callers wrap the returned error with context.
func ReadFileBounded(path string) ([]byte, error) {
	return fileio.ReadFileBounded(path)
}
