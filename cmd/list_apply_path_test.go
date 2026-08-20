package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchFileName_BuildsExpectedPath(t *testing.T) {
	got, err := patchFileName("hpa-patches", "default", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("hpa-patches", "default-web-hpa-patch.yaml")
	if got != want {
		t.Fatalf("patchFileName = %q, want %q", got, want)
	}
}

func TestPatchFileName_RejectsUnsafeIdentities(t *testing.T) {
	cases := []struct {
		namespace string
		name      string
	}{
		{"../etc", "web"},
		{"default", "../passwd"},
		{"a/b", "web"},
		{`a\b`, "web"},
		{"", "web"},
		{"default", ""},
		{".", "web"},
		{"default", ".."},
	}
	for _, tc := range cases {
		if _, err := patchFileName("hpa-patches", tc.namespace, tc.name); err == nil {
			t.Errorf("patchFileName(%q, %q): expected error, got nil", tc.namespace, tc.name)
		} else if !strings.Contains(err.Error(), "unsafe HPA identity") {
			t.Errorf("patchFileName(%q, %q): unexpected error %v", tc.namespace, tc.name, err)
		}
	}
}

func TestValidatePatchExportDirectory_RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(root, "hpa-patches")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	err := validatePatchExportDirectory(link)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestValidatePatchExportDirectory_RejectsNonDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hpa-patches")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := validatePatchExportDirectory(path)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected non-directory rejection, got %v", err)
	}
}

func TestValidatePatchExportDirectory_AcceptsMissingOrDirectory(t *testing.T) {
	root := t.TempDir()
	if err := validatePatchExportDirectory(filepath.Join(root, "absent")); err != nil {
		t.Fatalf("missing dir should be accepted: %v", err)
	}
	if err := validatePatchExportDirectory(root); err != nil {
		t.Fatalf("existing directory should be accepted: %v", err)
	}
}

func TestWritePatchExportFileRejectsDestinationSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, "default-web.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := writePatchExportFile(root, "default-web.yaml", []byte("replace")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("symlink target was modified: %q", got)
	}
}
