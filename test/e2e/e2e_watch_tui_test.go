//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"strings"
	"testing"

	// Import the root command package from our plugin
	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// TestE2E_WatchCommand verifies that the watch command runs with a short
// timeout and completes. A context deadline exceeded error is expected.
func TestE2E_WatchCommand(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "watch-rc")
	createHealthyHPA(t, client, nsName, "watch-hpa", "watch-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "watch-hpa", "-n", nsName, "--watch", "--timeout", "3s", "--interval", "1s", "--kubeconfig", kubeconfig})

	err := rootCmd.Execute()
	// The watch command is expected to terminate via context timeout.
	// Either a nil error (graceful exit) or a context.DeadlineExceeded is acceptable.
	if err != nil {
		t.Logf("Watch command returned error (expected on timeout): %v", err)
	}

	output := buf.String()
	t.Logf("Watch output (first 500 chars):\n%s", firstN(output, 500))

	// Verify at least one status output was produced.
	if !strings.Contains(output, "watch-hpa") {
		t.Errorf("expected watch-hpa in output, got:\n%s", firstN(output, 500))
	}
}

// TestE2E_TUICommand verifies the tui command can be constructed without error.
// Full TUI testing requires a terminal, so we only verify the command path.
func TestE2E_TUICommand(t *testing.T) {
	rootCmd := cmd.NewRootCommand()

	// Verify the tui subcommand exists
	tuiCmd, _, err := rootCmd.Find([]string{"tui"})
	if err != nil {
		t.Fatalf("tui subcommand not found: %v", err)
	}
	if tuiCmd == nil {
		t.Fatal("tui subcommand is nil")
	}
	if tuiCmd.Short == "" {
		t.Error("tui subcommand missing Short description")
	}
}
