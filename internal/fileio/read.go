// Package fileio contains bounded filesystem adapters shared by the CLI and
// analysis loaders.
package fileio

import (
	"fmt"
	"io"
	"os"
)

// MaxInputFileSize is the largest file accepted by shared input readers.
const MaxInputFileSize = 50 * 1024 * 1024

// ReadFileBounded reads path after rejecting files above MaxInputFileSize.
func ReadFileBounded(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- explicit user-provided input path
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file %s is not a regular file", path)
	}
	if info.Size() > MaxInputFileSize {
		return nil, fmt.Errorf("file %s is %d bytes, exceeding the %d MiB input limit", path, info.Size(), MaxInputFileSize/(1024*1024))
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxInputFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputFileSize {
		return nil, fmt.Errorf("file %s exceeds the %d MiB input limit while being read", path, MaxInputFileSize/(1024*1024))
	}
	return data, nil
}
