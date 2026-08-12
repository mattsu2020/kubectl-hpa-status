package fileio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileBounded(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small")
	if err := os.WriteFile(small, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFileBounded(small)
	if err != nil || string(got) != "ok" {
		t.Fatalf("ReadFileBounded() = %q, %v", got, err)
	}

	large := filepath.Join(dir, "large")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxInputFileSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := ReadFileBounded(large); err == nil || !strings.Contains(err.Error(), "input limit") {
		t.Fatalf("oversized file error = %v", err)
	}

	if _, err := ReadFileBounded(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
}
