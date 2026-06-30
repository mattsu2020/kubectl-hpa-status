package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

// commandFirstName extracts the first whitespace-delimited token from a
// command's Use string (e.g. "bundle NAME" -> "bundle"). Production callers
// were removed with the top-level alpha aliases in v2.0, but tests still use
// it, so it lives here as a shared test helper.
func commandFirstName(use string) string {
	for i, r := range use {
		if r == ' ' || r == '\t' {
			return use[:i]
		}
	}
	return use
}

// findSubCommandForTest returns the direct child of root with the given first
// Use word, or nil. Test-only helper kept here after the production version
// was removed alongside the top-level alpha aliases in v2.0.
func findSubCommandForTest(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if commandFirstName(c.Use) == name {
			return c
		}
	}
	return nil
}

// TestAlphaCommandGroupsAllAlphaSpecs verifies that every command listed in
// alphaCommandSpecs is registered under `alpha`, so the alpha tree and the
// spec registry cannot drift.
func TestAlphaCommandGroupsAllAlphaSpecs(t *testing.T) {
	root := NewRootCommand()
	alpha := findSubCommandForTest(root, "alpha")
	if alpha == nil {
		t.Fatal("expected an `alpha` subcommand on the root")
	}
	alphaChildren := map[string]bool{}
	for _, c := range alpha.Commands() {
		alphaChildren[commandFirstName(c.Use)] = true
	}
	// Each spec's constructor produces a command whose first Use word is the
	// canonical name; check it appears under alpha.
	for _, spec := range alphaCommandSpecs {
		c := spec.constructor(&options{})
		name := commandFirstName(c.Use)
		if !alphaChildren[name] {
			t.Errorf("alpha subcommand %q is not registered under alpha", name)
		}
	}
}

// TestAlphaSpecsCoverTestCategory confirms every spec declares a recognized
// category, so the alpha --help grouping stays consistent.
func TestAlphaSpecsCoverTestCategory(t *testing.T) {
	valid := map[string]bool{"operational": true, "experimental": true}
	for _, spec := range alphaCommandSpecs {
		if !valid[spec.category] {
			t.Errorf("alpha command spec has unknown category %q", spec.category)
		}
	}
}

// TestTopLevelAlphaAliasesRemoved verifies that the historical top-level
// aliases (policy, gitops, bundle, etc.) are NOT registered at the root in
// v2.0 — they live exclusively under `alpha`.
func TestTopLevelAlphaAliasesRemoved(t *testing.T) {
	root := NewRootCommand()
	rootChildren := map[string]bool{}
	for _, c := range root.Commands() {
		rootChildren[commandFirstName(c.Use)] = true
	}
	for _, spec := range alphaCommandSpecs {
		c := spec.constructor(&options{})
		name := commandFirstName(c.Use)
		if rootChildren[name] {
			t.Errorf("top-level alias %q should have been removed in v2.0 (it must live only under alpha)", name)
		}
	}
}

// TestAlphaCommandReachable verifies an alpha subcommand actually executes
// (not just registered), by checking alpha bundle --help renders.
func TestAlphaCommandReachable(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"alpha", "bundle", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("alpha bundle --help: %v", err)
	}
}
