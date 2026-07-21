package bundle

import (
	"errors"
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

// validateOutputPath refuses to write through an existing symlink so a bundle
// cannot be redirected onto an arbitrary file (for example a link planted in a
// shared directory pointing at a sensitive path). A not-yet-existing target is
// fine: WritePrivateFileAtomic creates it atomically.
func validateOutputPath(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output path: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write bundle through symlink %s", path)
	}
	return nil
}

// WritePrivateFileAtomic writes through a temporary file in the destination
// directory, fsyncs and closes it, then renames it into place. The callback
// must finish any nested writer (for example zip.Writer) before returning so
// its footer errors are included in the result.
func WritePrivateFileAtomic(path string, write func(io.Writer) error) (retErr error) {
	if err := validateOutputPath(path); err != nil {
		return err
	}
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
