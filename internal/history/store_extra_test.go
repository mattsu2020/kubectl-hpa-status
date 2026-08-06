package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStoreDir_WithXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	got, err := resolveStoreDir()
	if err != nil {
		t.Fatalf("resolveStoreDir() error: %v", err)
	}
	want := filepath.Join("/tmp/xdg-cache", "kubectl-hpa-status", "history")
	if got != want {
		t.Fatalf("resolveStoreDir() = %q, want %q", got, want)
	}
}

func TestResolveStoreDir_FallbackHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	got, err := resolveStoreDir()
	if err != nil {
		t.Fatalf("resolveStoreDir() error: %v", err)
	}
	want := filepath.Join(home, ".kubectl-hpa-status", "history")
	if got != want {
		t.Fatalf("resolveStoreDir() = %q, want %q", got, want)
	}
}

func TestNewHealthStore_CreatesXDGDirectory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	store, err := NewHealthStore()
	if err != nil {
		t.Fatalf("NewHealthStore() error: %v", err)
	}
	want := filepath.Join(base, "kubectl-hpa-status", "history")
	if store.Dir() != want {
		t.Fatalf("Dir() = %q, want %q", store.Dir(), want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("store directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("store path %q is not a directory", want)
	}
}

func TestHealthStoreDirGetter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}
	if store.Dir() != dir {
		t.Fatalf("Dir() = %q, want %q", store.Dir(), dir)
	}
}

func TestCorruptLinesError(t *testing.T) {
	err := &CorruptLinesError{Path: "/tmp/history/default_app.jsonl", Lines: []int{2, 5}}
	msg := err.Error()
	for _, want := range []string{"/tmp/history/default_app.jsonl", "2", "5", "corrupt"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("CorruptLinesError.Error() = %q, missing %q", msg, want)
		}
	}
}
