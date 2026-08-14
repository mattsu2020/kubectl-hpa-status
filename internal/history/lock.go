package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// acquireLock uses a portable O_EXCL lock file so separate kubectl processes
// cannot append while another process is pruning the same HPA history.
func acquireLock(path string) (func(), error) {
	return acquireLockContext(context.Background(), path)
}

// acquireLockContext acquires an ownership-aware lock. A heartbeat prevents a
// long-running live operation from being mistaken for an abandoned lock.
func acquireLockContext(ctx context.Context, path string) (func(), error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("creating history lock token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	for {
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
		if err == nil {
			if _, writeErr := fmt.Fprintf(lock, "%s %d\n", token, os.Getpid()); writeErr != nil {
				_ = lock.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("writing history lock: %w", writeErr)
			}
			if closeErr := lock.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("closing history lock: %w", closeErr)
			}
			stop := make(chan struct{})
			done := make(chan struct{})
			go heartbeatLock(lockPath, token, stop, done)
			return func() {
				close(stop)
				<-done
				removeLockIfOwned(lockPath, token)
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("creating history lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			quarantine := fmt.Sprintf("%s.stale-%s", lockPath, token)
			if renameErr := os.Rename(lockPath, quarantine); renameErr == nil {
				_ = os.Remove(quarantine)
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for history lock %s", lockPath)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for history lock %s: %w", lockPath, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func heartbeatLock(path, token string, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(lockHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			if !lockOwnedBy(path, token) {
				return
			}
			_ = os.Chtimes(path, now, now)
		}
	}
}

func removeLockIfOwned(path, token string) {
	if lockOwnedBy(path, token) {
		_ = os.Remove(path)
	}
}

func lockOwnedBy(path, token string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- internally derived lock path
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 0 && fields[0] == token
}
