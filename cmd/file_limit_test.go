package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileBounded_RejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	// Sparse file: Seek past the limit and write one byte so the stat size
	// exceeds maxInputFileSize without allocating it.
	if _, err := f.Seek(maxInputFileSize+1, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = readFileBounded(path)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "input limit") {
		t.Fatalf("expected input limit error, got: %v", err)
	}
}

func TestReadFileBounded_AcceptsSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.yaml")
	want := []byte("kind: HorizontalPodAutoscaler\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := readFileBounded(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestCollectManifestFiles_FiltersExtensionsAndSkipsVCS(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"a.yaml":              "kind: HPA",
		"b.yml":               "kind: HPA",
		"c.json":              "{}",
		"d.txt":               "ignore me",
		".git/config.yaml":    "should be skipped",
		"node_modules/x.yaml": "should be skipped",
		"vendor/y.yaml":       "should be skipped",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	got, err := collectManifestFiles(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := map[string]bool{}
	for _, f := range got {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		names[filepath.ToSlash(rel)] = true
	}

	for _, want := range []string{"a.yaml", "b.yml", "c.json"} {
		if !names[want] {
			t.Errorf("expected %s to be collected, got %v", want, names)
		}
	}
	for _, skip := range []string{"d.txt", ".git/config.yaml", "node_modules/x.yaml", "vendor/y.yaml"} {
		if names[skip] {
			t.Errorf("expected %s to be skipped, got %v", skip, names)
		}
	}
}

func TestCollectManifestFiles_DepthLimit(t *testing.T) {
	root := t.TempDir()
	// Create a file one level deeper than maxWalkDepth.
	deep := root
	for i := 0; i <= maxWalkDepth; i++ {
		deep = filepath.Join(deep, "level")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deep, "deep.yaml"), []byte("k: v"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := collectManifestFiles(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected deep file to be pruned, got %v", got)
	}
}

func TestCollectManifestFiles_SkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	target := filepath.Join(outside, "secret.yaml")
	if err := os.WriteFile(target, []byte("kind: HPA"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.yaml"), []byte("kind: HPA"), 0o600); err != nil {
		t.Fatalf("write real file: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.yaml")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	got, err := collectManifestFiles(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "real.yaml" {
		t.Fatalf("expected only real.yaml to be collected, got %v", got)
	}
}

func TestWalkDepth(t *testing.T) {
	tests := []struct {
		root string
		path string
		want int
	}{
		{"/a", "/a", 0},
		{"/a", "/a/b", 1},
		{"/a", "/a/b/c/d", 3},
	}
	for _, tc := range tests {
		if got := walkDepth(tc.root, tc.path); got != tc.want {
			t.Errorf("walkDepth(%q, %q) = %d, want %d", tc.root, tc.path, got, tc.want)
		}
	}
}

func TestSnapshotLimitError(t *testing.T) {
	err := snapshotLimitError("/tmp/records.jsonl")
	if err == nil {
		t.Fatal("snapshotLimitError returned nil error")
	}
	msg := err.Error()
	for _, want := range []string{"/tmp/records.jsonl", "snapshots per HPA"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("snapshotLimitError message %q missing %q", msg, want)
		}
	}
}
