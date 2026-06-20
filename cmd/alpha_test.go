package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestAlphaCommandGroupsAllAlphaSpecs verifies that every command listed in
// alphaCommandSpecs is registered under `alpha`, so the alpha tree and the
// spec registry cannot drift.
func TestAlphaCommandGroupsAllAlphaSpecs(t *testing.T) {
	root := NewRootCommand()
	alpha := findSubCommand(root, "alpha")
	if alpha == nil {
		t.Fatal("expected an `alpha` subcommand on the root")
	}
	alphaChildren := map[string]bool{}
	for _, c := range alpha.Commands() {
		alphaChildren[commandFirstName(c.Use)] = true
	}
	for _, spec := range alphaCommandSpecs {
		name := commandFirstName(spec.topLevelName)
		if !alphaChildren[name] {
			t.Errorf("alpha subcommand %q (from spec %q) is not registered under alpha", name, spec.topLevelName)
		}
	}
}

// TestAlphaSpecsCoverTestCategory confirms every spec declares a recognized
// category, so the alpha --help grouping stays consistent.
func TestAlphaSpecsCoverTestCategory(t *testing.T) {
	valid := map[string]bool{"operational": true, "experimental": true}
	for _, spec := range alphaCommandSpecs {
		if !valid[spec.category] {
			t.Errorf("alpha command %q has unknown category %q", spec.topLevelName, spec.category)
		}
	}
}

// TestTopLevelAlphaAliasesAreDeprecated verifies the historical top-level
// commands are marked Deprecated so users are redirected to the alpha path,
// while still being runnable (not removed).
func TestTopLevelAlphaAliasesAreDeprecated(t *testing.T) {
	root := NewRootCommand()
	for _, spec := range alphaCommandSpecs {
		name := commandFirstName(spec.topLevelName)
		cmd := findSubCommand(root, name)
		if cmd == nil {
			t.Errorf("top-level alias %q was removed; it should remain runnable but deprecated", name)
			continue
		}
		if cmd.Deprecated == "" {
			t.Errorf("top-level alias %q is not marked Deprecated; it should redirect to alpha %s", name, name)
		}
		if !strings.Contains(cmd.Deprecated, "alpha "+name) {
			t.Errorf("top-level alias %q deprecation %q does not mention the alpha replacement", name, cmd.Deprecated)
		}
	}
}

// TestAlphaCommandReachable verifies an alpha subcommand actually executes
// (not just registered), by checking alpha bundle --help renders without the
// deprecation notice that the top-level alias carries.
func TestAlphaCommandReachable(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"alpha", "bundle", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("alpha bundle --help: %v", err)
	}
	output := out.String()
	if strings.Contains(output, "is deprecated") {
		t.Fatalf("alpha path should not carry the top-level deprecation notice:\n%s", output)
	}
}

// TestTopLevelAliasShowsDeprecationNotice verifies the deprecated top-level
// alias surfaces the Cobra deprecation message on invocation.
func TestTopLevelAliasShowsDeprecationNotice(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"flap", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("flap --help: %v", err)
	}
	if !strings.Contains(out.String(), "deprecated") {
		t.Fatalf("top-level flap alias should show a deprecation notice:\n%s", out.String())
	}
}
