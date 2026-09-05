//go:build !windows

package history

import (
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

// unlockFile releases the advisory lock taken by tryLockFile.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
