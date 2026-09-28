package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagTokenPattern matches long-flag spellings as they appear in the docs
// (e.g. "--from-record", "--output-schema"). Values and shorthand markers are
// not captured.
var flagTokenPattern = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// docFiles are the user-facing documents whose flag spellings must match the
// CLI. README.ja.md documents the same flags as README.md.
var docFiles = []string{
	"../docs/usage.md",
	"../docs/reference.md",
	"../README.md",
	"../README.ja.md",
}

// collectCommandFlags walks the command tree and returns every registered
// flag name (local and persistent, including deprecated aliases). Cobra
// registers --help/--version lazily at execution time, so the defaults are
// initialized first.
func collectCommandFlags(cmd *cobra.Command, into map[string]bool) {
	cmd.InitDefaultHelpFlag()
	cmd.InitDefaultVersionFlag()
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) { into[f.Name] = true })
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) { into[f.Name] = true })
	for _, sub := range cmd.Commands() {
		collectCommandFlags(sub, into)
	}
}

// nonFlagLine reports whether a documentation line only LOOKS like it carries
// a flag: shields.io badge URLs contain "--lint-blue"-shaped substrings.
func nonFlagLine(line string) bool {
	return strings.Contains(line, "img.shields.io")
}

// TestDocumentedFlagsExist pins that every --flag mentioned in the docs is a
// real CLI flag. It exists because docs/usage.md documented a --raise-max
// flag that no command ever registered, so the advertised command failed with
// "unknown flag" and the README sync script could not catch it (it only
// compares the two READMEs).
func TestDocumentedFlagsExist(t *testing.T) {
	known := map[string]bool{}
	collectCommandFlags(NewRootCommand(), known)

	// Flags documented for the surrounding kubectl ecosystem, not this plugin:
	// metrics-server install options in the e2e notes, kubectl patch flags
	// embedded inside copy-paste strings, and the removed-in-v2 flag names that
	// the migration table in docs/reference.md intentionally lists.
	external := map[string]bool{
		"--kubelet-insecure-tls": true,
		"--dry-run":              true, // kubectl patch examples embed it inside patch commands
		"--type":                 true, // kubectl patch --type=merge inside copy-paste strings
		"--recommend":            true, // removed in v2.0; kept in the migration table
		"--export-patch":         true, // removed in v2.0; kept in the migration table
		"--max-score":            true, // removed in v2.0; kept in the migration table
	}

	for _, path := range docFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		seen := map[string]bool{}
		for _, line := range strings.Split(string(data), "\n") {
			if nonFlagLine(line) {
				continue
			}
			for _, token := range flagTokenPattern.FindAllString(line, -1) {
				if seen[token] {
					continue
				}
				seen[token] = true
				if external[token] {
					continue
				}
				if !known[strings.TrimPrefix(token, "--")] {
					t.Errorf("%s: flag %q is documented but not registered by any command", path, token)
				}
			}
		}
	}
}
