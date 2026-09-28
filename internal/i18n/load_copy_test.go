package i18n

import (
	"testing"
)

// TestLoadReturnsDefensiveCopy pins the contract that mutating the bundle
// returned by Load cannot corrupt the process-wide translations other
// goroutines (including the alt-screen TUI) read.
func TestLoadReturnsDefensiveCopy(t *testing.T) {
	first := Load("en")
	key := "list_header_name"
	original, ok := first[key]
	if !ok {
		t.Fatalf("expected key %q in the English bundle", key)
	}

	first[key] = "mutated"
	second := Load("en")
	if got := second[key]; got != original {
		t.Fatalf("mutating a returned bundle leaked into the process-wide bundle: %q != %q", got, original)
	}
	// Restore for other tests in this package anyway.
	delete(first, key)
}
