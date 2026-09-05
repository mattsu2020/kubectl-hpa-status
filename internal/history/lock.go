package history

import (
	"context"
	"fmt"
	"os"
	"time"
)

// acquireLock serializes history-file access between processes with an OS
// advisory lock on a sidecar "<path>.lock" file (see lock_unix.go /
// lock_windows.go for the per-platform primitive).
//
// Ownership is enforced by the kernel: the lock disappears when the owning
// process exits, whether or not it released cleanly. There is therefore no
// stale-lock reclamation pass, and the reclaim race of the previous
// O_EXCL+heartbeat design — where a slow writer's fresh lock could be judged
// abandoned from its mtime and stolen by a concurrent process — is gone by
// construction.
//
// The lock file itself is intentionally left in place after release. Removing
// it would reintroduce a race: a waiter can hold a lock on the now-unlinked
// inode while a third process creates and locks a fresh file, ending with two
// "owners". An empty leftover dotfile in the cache directory is harmless.
func acquireLock(path string) (func(), error) {
	return acquireLockContext(context.Background(), path)
}

// acquireLockContext acquires the exclusive history lock, waiting up to
// lockTimeout for a competing holder and honouring ctx cancellation while
// waiting.
func acquireLockContext(ctx context.Context, path string) (func(), error) {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, storeFileMode)
	if err != nil {
		return nil, fmt.Errorf("opening history lock: %w", err)
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		err := tryLockFile(f)
		if err == nil {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("timed out waiting for history lock %s", lockPath)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("waiting for history lock %s: %w", lockPath, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
