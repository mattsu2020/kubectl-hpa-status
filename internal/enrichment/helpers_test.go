package enrichment

import "testing"

func TestRequested(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"on", true},
		{"auto", true},
		{"true", true},
		{"1", true},
		{"off", false},
		{"false", false},
		{"0", false},
		{"", false},
		{"unknown-value", false},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			if got := requested(tc.mode); got != tc.want {
				t.Fatalf("requested(%q) = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		crdPresent bool
		want       bool
	}{
		{"on forces enable regardless of CRD", "on", false, true},
		{"true forces enable", "true", false, true},
		{"1 forces enable", "1", true, true},
		{"off disables regardless of CRD", "off", true, false},
		{"empty disables", "", true, false},
		{"auto enables when CRD present", "auto", true, true},
		{"auto disables when CRD absent", "auto", false, false},
		{"unknown falls back to CRD presence", "weird", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEnabled(tc.mode, tc.crdPresent); got != tc.want {
				t.Fatalf("isEnabled(%q, crd=%v) = %v, want %v", tc.mode, tc.crdPresent, got, tc.want)
			}
		})
	}
}

func TestApplyCRDAvailability(t *testing.T) {
	t.Run("not requested is a no-op", func(t *testing.T) {
		entry := &Entry{State: StateDisabled, Reason: "initial"}
		applyCRDAvailability(entry, false, true, "missing")
		if entry.State != StateDisabled || entry.Reason != "initial" {
			t.Fatalf("expected no-op, got State=%v Reason=%q", entry.State, entry.Reason)
		}
	})

	t.Run("requested and available preserves prior reason", func(t *testing.T) {
		entry := &Entry{State: StateDisabled, Reason: "initial"}
		applyCRDAvailability(entry, true, true, "missing")
		if entry.State != StateUnavailable {
			t.Fatalf("expected StateUnavailable, got %v", entry.State)
		}
		// When available, the function only sets State and does not touch Reason,
		// so a pre-existing reason is preserved.
		if entry.Reason != "initial" {
			t.Fatalf("expected prior reason preserved, got %q", entry.Reason)
		}
	})

	t.Run("requested but missing sets reason", func(t *testing.T) {
		entry := &Entry{State: StateDisabled, Reason: "initial"}
		applyCRDAvailability(entry, true, false, "CRD not found")
		if entry.State != StateUnavailable {
			t.Fatalf("expected StateUnavailable, got %v", entry.State)
		}
		if entry.Reason != "CRD not found" {
			t.Fatalf("expected missing reason, got %q", entry.Reason)
		}
	})
}

func TestSetEnrichmentError(t *testing.T) {
	t.Run("disabled entry unchanged", func(t *testing.T) {
		entry := &Entry{State: StateDisabled}
		setEnrichmentError(entry, false, "boom")
		if entry.State != StateDisabled {
			t.Fatalf("expected StateDisabled, got %v", entry.State)
		}
		if entry.Reason != "" {
			t.Fatalf("expected empty reason, got %q", entry.Reason)
		}
	})

	t.Run("enabled entry records error", func(t *testing.T) {
		entry := &Entry{State: StateUnavailable}
		setEnrichmentError(entry, true, "boom")
		if entry.State != StateError {
			t.Fatalf("expected StateError, got %v", entry.State)
		}
		if entry.Reason != "boom" {
			t.Fatalf("expected 'boom', got %q", entry.Reason)
		}
	})
}

func TestContextStatus_NilSafe(t *testing.T) {
	// Calling Status() on a nil Context must not panic and return zero value.
	var nilCtx *Context
	if got := nilCtx.Status(); got.KEDA != nil || got.VPA != nil {
		t.Fatalf("expected zero Status from nil Context, got %+v", got)
	}
}
