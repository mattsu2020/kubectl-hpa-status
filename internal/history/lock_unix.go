//go:build !windows

package history

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes an exclusive advisory lock on the file without blocking.
// It reports an error when another process (or goroutine, via a separate file
// description) already holds the lock.
func tryLockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == syscall.EINTR {
			continue
		}
		return err
	}
}

// isLockContention reports whether err indicates the lock is currently held by
// another process rather than a permanent filesystem/OS error.
func isLockContention(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

// unlockFile releases the advisory lock taken by tryLockFile.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
