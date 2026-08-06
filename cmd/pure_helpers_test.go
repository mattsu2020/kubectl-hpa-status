package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/errs"
)

func TestNoSnapshotsErrorWrapper(t *testing.T) {
	err := noSnapshotsError("prod", "web")
	if err == nil {
		t.Fatal("noSnapshotsError returned nil")
	}
	if !errors.Is(err, errs.ErrNoRecordedSnapshots) {
		t.Fatalf("noSnapshotsError should wrap ErrNoRecordedSnapshots, got %v", err)
	}
}

func TestValidAnalysisProfiles(t *testing.T) {
	got := validAnalysisProfiles()
	if len(got) == 0 {
		t.Fatal("validAnalysisProfiles returned empty list")
	}
	if !containsString(got, "standard") {
		t.Fatalf("validAnalysisProfiles missing standard profile: %v", got)
	}
}

func containsString(xs []string, want string) bool {
	for _, v := range xs {
		if v == want {
			return true
		}
	}
	return false
}

func TestWriteAIContextError(t *testing.T) {
	var out bytes.Buffer
	if err := writeAIContextError(&out, "ns", "app", errors.New("boom")); err != nil {
		t.Fatalf("writeAIContextError returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"ns/app", "boom"} {
		if !strings.Contains(got, want) {
			t.Fatalf("writeAIContextError output missing %q: %q", want, got)
		}
	}
}
