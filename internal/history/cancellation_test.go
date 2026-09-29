package history

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
)

func TestCancelledAppendLeavesStoreUntouched(t *testing.T) {
	s, e := NewHealthStoreWithDir(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e = s.Append(ctx, SnapshotKey{Cluster: "c", Namespace: "default", Name: "web", UID: "u"}, healthtrend.HealthSnapshot{Timestamp: time.Now()})
	if !errors.Is(e, context.Canceled) {
		t.Errorf("cancelled Append: err=%v", e)
	}
	files, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("cancelled Append created files: %v", files)
	}
}

func TestCancelledAppendPreservesExistingHistory(t *testing.T) {
	store, err := NewHealthStoreWithDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := SnapshotKey{Cluster: "c", Namespace: "default", Name: "web", UID: "u"}
	snapshot := healthtrend.HealthSnapshot{Timestamp: time.Now()}
	if err := store.Append(context.Background(), key, snapshot); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.filePath(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Append(ctx, key, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	after, err := os.ReadFile(store.filePath(key))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("cancelled append changed existing history")
	}
	// A subsequent operation must still acquire the lock successfully.
	if err := store.Append(context.Background(), key, snapshot); err != nil {
		t.Fatal(err)
	}
}
