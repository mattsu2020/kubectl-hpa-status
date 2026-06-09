package history

import (
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestHealthStoreAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	now := time.Now()
	snapshots := []hpa.HealthSnapshot{
		{Timestamp: now.Add(-2 * time.Hour), HealthScore: 100, HealthState: "OK", DesiredReplicas: 5, CurrentReplicas: 5},
		{Timestamp: now.Add(-1 * time.Hour), HealthScore: 80, HealthState: "LIMITED", DesiredReplicas: 8, CurrentReplicas: 6},
		{Timestamp: now, HealthScore: 90, HealthState: "OK", DesiredReplicas: 7, CurrentReplicas: 7},
	}

	// Append all snapshots.
	for _, snap := range snapshots {
		if err := store.Append("default", "my-app", snap); err != nil {
			t.Fatalf("Append() error: %v", err)
		}
	}

	// Load with 3-hour window — should get all 3.
	loaded, err := store.Load("default", "my-app", 3*time.Hour)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("Load() returned %d snapshots, want 3", len(loaded))
	}

	// Load with 90-minute window — should get only 2.
	loaded, err = store.Load("default", "my-app", 90*time.Minute)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("Load() returned %d snapshots, want 2", len(loaded))
	}
}

func TestHealthStoreLoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	loaded, err := store.Load("default", "nonexistent", 24*time.Hour)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("Load() returned %d snapshots for nonexistent HPA, want 0", len(loaded))
	}
}

func TestHealthStorePrune(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	now := time.Now()
	old := hpa.HealthSnapshot{Timestamp: now.Add(-48 * time.Hour), HealthScore: 50, HealthState: "ERROR"}
	recent := hpa.HealthSnapshot{Timestamp: now.Add(-1 * time.Hour), HealthScore: 100, HealthState: "OK"}

	if err := store.Append("default", "my-app", old); err != nil {
		t.Fatalf("Append() error: %v", err)
	}
	if err := store.Append("default", "my-app", recent); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	// Prune entries older than 24 hours.
	if err := store.Prune("default", "my-app", 24*time.Hour); err != nil {
		t.Fatalf("Prune() error: %v", err)
	}

	loaded, err := store.Load("default", "my-app", 72*time.Hour)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("after Prune(), Load() returned %d snapshots, want 1", len(loaded))
	}
	if loaded[0].HealthScore != 100 {
		t.Errorf("remaining snapshot HealthScore = %d, want 100", loaded[0].HealthScore)
	}
}

func TestHealthStoreEmptyNamespaceRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	snap := hpa.HealthSnapshot{Timestamp: time.Now(), HealthScore: 100, HealthState: "OK"}
	if err := store.Append("", "my-app", snap); err == nil {
		t.Error("expected error for empty namespace")
	}
	if err := store.Append("default", "", snap); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestHealthStoreLoadMultiple(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHealthStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewHealthStoreWithDir() error: %v", err)
	}

	now := time.Now()
	store.Append("default", "app-a", hpa.HealthSnapshot{Timestamp: now, HealthScore: 90, HealthState: "OK"})
	store.Append("default", "app-b", hpa.HealthSnapshot{Timestamp: now, HealthScore: 80, HealthState: "OK"})

	keys := []struct{ NS, Name string }{
		{NS: "default", Name: "app-a"},
		{NS: "default", Name: "app-b"},
		{NS: "default", Name: "app-c"}, // nonexistent
	}

	result, err := store.LoadMultiple(keys, 1*time.Hour)
	if err != nil {
		t.Fatalf("LoadMultiple() error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("LoadMultiple() returned %d entries, want 2", len(result))
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "default", want: "default"},
		{input: "kube-system", want: "kube-system"},
		{input: "../../../etc/passwd", want: "_.._.._.._etc_passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
