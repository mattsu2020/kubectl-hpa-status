package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOutputPath_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sensitive.txt")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "bundle.zip")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	err := WritePrivateFile(link, []byte("attacker content"))
	if err == nil {
		t.Fatal("expected error when writing through a symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got: %v", err)
	}

	// The symlink target must be untouched.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("symlink target was overwritten: %q", got)
	}
}

func TestValidateOutputPath_AllowsRegularFileAndMissingPath(t *testing.T) {
	dir := t.TempDir()

	// Missing path is fine (will be created).
	if err := validateOutputPath(filepath.Join(dir, "new.md")); err != nil {
		t.Fatalf("missing path should be allowed: %v", err)
	}

	// Existing regular file is fine (will be atomically replaced).
	existing := filepath.Join(dir, "existing.md")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := validateOutputPath(existing); err != nil {
		t.Fatalf("regular file should be allowed: %v", err)
	}
}
