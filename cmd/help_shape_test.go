package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestRootHelpListsCoreCommands verifies the root --help output surfaces the
// basic and investigation commands a daily user needs, and that the alpha
// grouping is visible. This is a shape test (no golden file, per project
// convention): it catches a command being accidentally dropped from the root
// tree or renamed, because the help text would then lose the command name.
func TestRootHelpListsCoreCommands(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("root --help: %v", err)
	}
	help := out.String()

	// Basic + investigation commands must be visible at the top level.
	for _, cmd := range []string{"status", "list", "scan", "doctor", "watch", "explain", "tui", "trace", "timeline", "recommend"} {
		if !strings.Contains(help, cmd) {
			t.Errorf("root --help does not mention core command %q", cmd)
		}
	}
	// The alpha grouping must appear so users discover the grouped commands.
	if !strings.Contains(help, "alpha") {
		t.Errorf("root --help does not mention the alpha command group")
	}
}

// TestStatusHelpListsDepthTiers verifies the status --help output advertises
// the depth-tier flags added for the lightweight-status work, so users learn
// --deep / --no-enrich / --hpa-only exist without reading the README.
func TestStatusHelpListsDepthTiers(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status --help: %v", err)
	}
	help := out.String()
	for _, flag := range []string{"--deep", "--no-enrich", "--hpa-only", "--explain-pods"} {
		if !strings.Contains(help, flag) {
			t.Errorf("status --help does not document %s", flag)
		}
	}
}

// TestAlphaHelpListsGroupedCommands verifies the alpha --help output lists
// every grouped command, so the alpha path is self-documenting.
func TestAlphaHelpListsGroupedCommands(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"alpha", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("alpha --help: %v", err)
	}
	help := out.String()
	for _, spec := range alphaCommandSpecs {
		c := spec.constructor(&options{})
		name := commandFirstName(c.Use)
		if !strings.Contains(help, name) {
			t.Errorf("alpha --help does not list grouped command %q", name)
		}
	}
}
