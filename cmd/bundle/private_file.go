package bundle

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const privateFileMode = 0o600

// WritePrivateFile atomically replaces path with data using owner-only
// permissions. Diagnostic bundles can contain workload configuration and must
// not inherit the process umask's usual world-readable output mode.
func WritePrivateFile(path string, data []byte) error {
	return WritePrivateFileAtomic(path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// WritePrivateFileAtomic writes through a temporary file in the destination
// directory, fsyncs and closes it, then renames it into place. The callback
// must finish any nested writer (for example zip.Writer) before returning so
// its footer errors are included in the result.
func WritePrivateFileAtomic(path string, write func(io.Writer) error) (retErr error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(privateFileMode); err != nil {
		return fmt.Errorf("set private output permissions: %w", err)
	}
	if err := write(tmp); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync output: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace output: %w", err)
	}
	return nil
}
