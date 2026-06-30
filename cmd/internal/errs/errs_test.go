package errs

import (
	"errors"
	"strings"
	"testing"
)

func TestNoSnapshotsError(t *testing.T) {
	t.Run("with namespace and name wraps sentinel with ns/name message", func(t *testing.T) {
		err := NoSnapshotsError("production", "web")
		msg := err.Error()
		if !strings.Contains(msg, "record file has no snapshots for production/web") {
			t.Fatalf("expected ns/name message, got: %s", msg)
		}
		if !errors.Is(err, ErrNoRecordedSnapshots) {
			t.Fatal("expected ErrNoRecordedSnapshots to be reachable via errors.Is")
		}
	})

	t.Run("empty namespace uses the legacy namespace phrasing", func(t *testing.T) {
		err := NoSnapshotsError("", "")
		msg := err.Error()
		if !strings.Contains(msg, "record file has no snapshots for namespace ") {
			t.Fatalf("expected legacy namespace phrasing, got: %s", msg)
		}
		if !errors.Is(err, ErrNoRecordedSnapshots) {
			t.Fatal("expected ErrNoRecordedSnapshots for empty-namespace case too")
		}
	})
}

func TestSentinelsAreDistinct(t *testing.T) {
	// Guards against accidental sentinel aliasing during refactors.
	all := []error{ErrHPANotFound, ErrNoRecordedSnapshots, ErrPolicyViolations, ErrPolicyGuardBlocked, ErrInvalidCandidateSpec}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinels %v and %v must be distinct", a, b)
			}
		}
	}
}
