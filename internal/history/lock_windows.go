//go:build windows

package history

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile takes an exclusive lock on the whole file range without
// blocking. LockFileEx reports ERROR_LOCK_VIOLATION when another process
// already holds the lock; any error is treated as "not acquired" by the
// caller's retry loop.
func tryLockFile(f *os.File) error {
	overlapped := new(windows.Overlapped)
	const (
		lockWholeRangeLow  = ^uint32(0)
		lockWholeRangeHigh = ^uint32(0)
	)
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		lockWholeRangeLow,
		lockWholeRangeHigh,
		overlapped,
	)
}

// unlockFile releases the range lock taken by tryLockFile.
func unlockFile(f *os.File) error {
	overlapped := new(windows.Overlapped)
	const (
		unlockWholeRangeLow  = ^uint32(0)
		unlockWholeRangeHigh = ^uint32(0)
	)
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		unlockWholeRangeLow,
		unlockWholeRangeHigh,
		overlapped,
	)
}
