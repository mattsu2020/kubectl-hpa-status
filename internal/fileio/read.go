// Package fileio contains bounded filesystem adapters shared by the CLI and
// analysis loaders.
package fileio

import (
	"fmt"
	"os"
)

const MaxInputFileSize = 50 * 1024 * 1024

func ReadFileBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxInputFileSize {
		return nil, fmt.Errorf("file %s is %d bytes, exceeding the %d MiB input limit", path, info.Size(), MaxInputFileSize/(1024*1024))
	}
	return os.ReadFile(path) // #nosec G304 -- explicit user path with size bound
}
