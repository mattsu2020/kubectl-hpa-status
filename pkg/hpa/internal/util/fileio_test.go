package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileBounded_RejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	// Sparse file: seek past the limit and write one byte so the stat size
	// exceeds MaxInputFileSize without allocating it.
	if _, err := f.Seek(MaxInputFileSize+1, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := ReadFileBounded(path); err == nil {
		t.Fatal("expected error for oversized file")
	} else if !strings.Contains(err.Error(), "input limit") {
		t.Fatalf("expected input limit error, got: %v", err)
	}
}

func TestReadFileBounded_AcceptsSmallFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.json")
	want := []byte(`{"snapshots": []}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := ReadFileBounded(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestReadFileBounded_MissingFile(t *testing.T) {
	if _, err := ReadFileBounded(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
