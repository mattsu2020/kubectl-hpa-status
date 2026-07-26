//go:build !windows

package cmd

import "os"

// syncRecordDirectory makes the atomic rename durable on filesystems that
// require the containing directory entry to be flushed separately.
func syncRecordDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
