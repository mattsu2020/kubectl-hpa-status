//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"errors"
	"testing"

	// Import the root command package from our plugin
	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// TestE2E_ConfigFile verifies that a custom config file with non-default
// health weights produces a different health score compared to default weights.
func TestE2E_ConfigFile(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "config-rc")
	// Use a broken HPA so that health weights have a visible effect.
	createBrokenHPA(t, client, nsName, "config-hpa", "config-rc")

	// Step 1: Get the default health score.
	defaultBuf := new(bytes.Buffer)
	defaultCmd := cmd.NewRootCommand()
	defaultCmd.SetOut(defaultBuf)
	defaultCmd.SetErr(defaultBuf)
	defaultCmd.SetArgs([]string{"status", "config-hpa", "-n", nsName, "-o", "json", "--kubeconfig", kubeconfig})

	if err := defaultCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("default command error: %v", err)
		}
	}

	defaultScore := parseHealthScore(t, defaultBuf.String())
	t.Logf("Default health score: %d", defaultScore)

	// Step 2: Get the score with a custom config that sets scalingInactive to 10 (much lower penalty).
	configContent := `healthWeights:
  scalingInactive: 10
`
	configPath := writeTempConfigFile(t, configContent)

	configBuf := new(bytes.Buffer)
	configCmd := cmd.NewRootCommand()
	configCmd.SetOut(configBuf)
	configCmd.SetErr(configBuf)
	configCmd.SetArgs([]string{"status", "config-hpa", "-n", nsName, "-o", "json", "--config", configPath, "--kubeconfig", kubeconfig})

	if err := configCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("config command error: %v", err)
		}
	}

	configScore := parseHealthScore(t, configBuf.String())
	t.Logf("Custom config health score: %d", configScore)

	// The custom config sets scalingInactive=10 instead of the default 45,
	// so the health score should be higher (less penalty).
	if configScore <= defaultScore {
		t.Errorf("expected config score (%d) to be greater than default score (%d) with reduced scalingInactive penalty", configScore, defaultScore)
	}
}
