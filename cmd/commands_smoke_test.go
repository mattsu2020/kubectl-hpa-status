package cmd

import (
	"context"
	"testing"
)

// Smoke tests for thin command constructors: assert the cobra metadata (Use,
// Short, required flags) stays stable across refactors. These commands'
// runtime behavior is exercised via the runXxx functions they delegate to,
// which are covered by their own per-command test files.

func TestNewExportCommand(t *testing.T) {
	c := newExportCommand(&options{})
	if c.Use != "export" {
		t.Fatalf("Use = %q, want export", c.Use)
	}
	if c.Short == "" {
		t.Fatal("Short must be non-empty")
	}
	if c.Args == nil {
		t.Fatal("Args validator must be set")
	}
	if c.Flags().Lookup("prometheus") == nil {
		t.Fatal("expected --prometheus flag")
	}
}

func TestNewSupportBundleCommand(t *testing.T) {
	c := newSupportBundleCommand(&options{})
	if c.Use == "" {
		t.Fatal("Use must be non-empty")
	}
	if c.Short == "" {
		t.Fatal("Short must be non-empty")
	}
	// support-bundle requires a NAME positional arg.
	if c.Args == nil {
		t.Fatal("Args validator must be set")
	}
	redact, err := c.Flags().GetBool("redact")
	if err != nil {
		t.Fatalf("read --redact: %v", err)
	}
	if !redact {
		t.Fatal("support bundles must default to redaction")
	}
}

func TestNewEnrichmentContextNilOptsSafe(t *testing.T) {
	// newEnrichmentContext must not panic when options fields are at their zero
	// value; it is called from many status-adjacent paths and the KEDA/VPA
	// toggles default off.
	ec := newEnrichmentContext(context.Background(), &options{})
	if ec == nil {
		t.Fatal("expected non-nil enrichment context for default options")
	}
}
