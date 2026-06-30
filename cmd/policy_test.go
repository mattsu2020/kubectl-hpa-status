package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot resolve home dir: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "bare tilde resolves to home", path: "~", want: home},
		{name: "tilde slash prefix expands", path: "~/config/policy.yaml", want: filepath.Join(home, "config/policy.yaml")},
		{name: "absolute path unchanged", path: "/etc/policy.yaml", want: "/etc/policy.yaml"},
		{name: "relative path unchanged", path: "policy.yaml", want: "policy.yaml"},
		{name: "empty path unchanged", path: "", want: ""},
		{name: "tilde mid-path not expanded", path: "a/~b", want: "a/~b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandHomePath(tt.path); got != tt.want {
				t.Fatalf("expandHomePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestWritePolicyProfile(t *testing.T) {
	t.Run("known profile renders rules", func(t *testing.T) {
		var sb strings.Builder
		if err := writePolicyProfile(&sb, "production-api"); err != nil {
			t.Fatalf("writePolicyProfile: %v", err)
		}
		out := sb.String()
		// Every known profile body carries HPA policy rule scaffolding.
		if !strings.Contains(out, "apiVersion: hpa-status/v1") {
			t.Fatalf("expected apiVersion header, got:\n%s", out)
		}
		if !strings.Contains(out, "rules:") {
			t.Fatalf("expected rules section, got:\n%s", out)
		}
	})

	t.Run("default profile is production-api", func(t *testing.T) {
		// An empty profile name selects the default profile, which must still
		// render valid policy scaffolding rather than erroring.
		var sb strings.Builder
		if err := writePolicyProfile(&sb, ""); err != nil {
			t.Fatalf("writePolicyProfile default: %v", err)
		}
		if sb.Len() == 0 {
			t.Fatal("expected non-empty output for default profile")
		}
	})

	t.Run("unknown profile errors", func(t *testing.T) {
		var sb strings.Builder
		err := writePolicyProfile(&sb, "does-not-exist")
		if err == nil {
			t.Fatal("expected error for unknown profile, got nil")
		}
	})
}
