//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// TestE2E_ScalingLimitedMaxReplicas exercises the ScalingLimited=True /
// maxReplicas-reached failure path (Issue #10 priority #2). It verifies:
//   - status --explain calls out the maxReplicas cap;
//   - list --problem surfaces the capped HPA.
//
// The ScalingLimited HPA is built by the shared createScalingLimitedHPA helper
// in e2e_test.go.
func TestE2E_ScalingLimitedMaxReplicas(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "limited-rc")
	createScalingLimitedHPA(t, client, nsName, "limited-hpa", "limited-rc")

	// status --explain should mention the maxReplicas cap.
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "limited-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})
	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}
	explainOut := buf.String()
	t.Logf("ScalingLimited explain output:\n%s", explainOut)
	if !strings.Contains(strings.ToLower(explainOut), "maxreplicas") {
		t.Errorf("expected explain output to mention maxReplicas, got:\n%s", explainOut)
	}

	// list --problem should surface the capped HPA.
	buf.Reset()
	rootCmd = cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-n", nsName, "--problem", "--kubeconfig", kubeconfig})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --problem: %v. Output:\n%s", err, buf.String())
	}
	problemOut := buf.String()
	t.Logf("ScalingLimited list --problem output:\n%s", problemOut)
	if !strings.Contains(problemOut, "limited-hpa") {
		t.Errorf("expected limited-hpa in --problem output, got:\n%s", problemOut)
	}
}
