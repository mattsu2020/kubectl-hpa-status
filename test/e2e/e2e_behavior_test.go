//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// behaviorJSON mirrors the machine-readable shape of the behavior subcommand.
// Kept local to the e2e package so the test does not depend on unexported
// cmd types.
type behaviorJSON struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	ScaleUp   struct {
		StabilizationWindowSeconds int32  `json:"stabilizationWindowSeconds"`
		SelectPolicy               string `json:"selectPolicy"`
		Policies                   []struct {
			Type          string `json:"type"`
			Value         int32  `json:"value"`
			PeriodSeconds int32  `json:"periodSeconds"`
		} `json:"policies"`
	} `json:"scaleUp"`
	ScaleDown struct {
		StabilizationWindowSeconds int32  `json:"stabilizationWindowSeconds"`
		SelectPolicy               string `json:"selectPolicy"`
		Policies                   []struct {
			Type          string `json:"type"`
			Value         int32  `json:"value"`
			PeriodSeconds int32  `json:"periodSeconds"`
		} `json:"policies"`
	} `json:"scaleDown"`
}

// TestE2E_BehaviorPolicies covers the ROADMAP "behavior policies" E2E gap:
// an HPA with explicit scaleUp/scaleDown policies is created, then both
// `behavior -o json` and `status --explain` are exercised so policy windows
// and selectPolicy values are visible to operators.
func TestE2E_BehaviorPolicies(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "behavior-rc")
	createBehaviorPolicyHPA(t, client, nsName, "behavior-hpa", "behavior-rc")

	// --- behavior subcommand (structured) ---
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"behavior", "behavior-hpa",
		"-n", nsName,
		"-o", "json",
		"--kubeconfig", kubeconfig,
	})
	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("behavior command failed: %v\noutput:\n%s", err, buf.String())
		}
	}

	var result behaviorJSON
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("behavior -o json is not valid JSON: %v\nraw:\n%s", err, buf.String())
	}
	if result.Name != "behavior-hpa" {
		t.Errorf("Name = %q, want behavior-hpa", result.Name)
	}
	if result.ScaleUp.StabilizationWindowSeconds != 0 {
		t.Errorf("scaleUp.stabilizationWindowSeconds = %d, want 0", result.ScaleUp.StabilizationWindowSeconds)
	}
	if !strings.EqualFold(result.ScaleUp.SelectPolicy, "Max") {
		t.Errorf("scaleUp.selectPolicy = %q, want Max", result.ScaleUp.SelectPolicy)
	}
	if len(result.ScaleUp.Policies) < 2 {
		t.Fatalf("scaleUp.policies len = %d, want >= 2", len(result.ScaleUp.Policies))
	}
	if result.ScaleDown.StabilizationWindowSeconds != 180 {
		t.Errorf("scaleDown.stabilizationWindowSeconds = %d, want 180", result.ScaleDown.StabilizationWindowSeconds)
	}
	if !strings.EqualFold(result.ScaleDown.SelectPolicy, "Min") {
		t.Errorf("scaleDown.selectPolicy = %q, want Min", result.ScaleDown.SelectPolicy)
	}
	if len(result.ScaleDown.Policies) < 1 {
		t.Fatalf("scaleDown.policies len = %d, want >= 1", len(result.ScaleDown.Policies))
	}

	// --- status --explain should surface behavior rules in text ---
	statusBuf := new(bytes.Buffer)
	statusCmd := cmd.NewRootCommand()
	statusCmd.SetOut(statusBuf)
	statusCmd.SetErr(statusBuf)
	statusCmd.SetArgs([]string{
		"status", "behavior-hpa",
		"-n", nsName,
		"--explain",
		"--kubeconfig", kubeconfig,
	})
	if err := statusCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("status --explain failed: %v\noutput:\n%s", err, statusBuf.String())
		}
	}
	statusOut := statusBuf.String()
	t.Logf("status --explain output:\n%s", statusOut)
	// Behavior section typically mentions scaleUp/scaleDown or stabilization.
	if !strings.Contains(statusOut, "scaleUp") &&
		!strings.Contains(statusOut, "scaleDown") &&
		!strings.Contains(statusOut, "stabilization") &&
		!strings.Contains(statusOut, "Behavior") {
		t.Errorf("expected behavior policy signals in status --explain output, got:\n%s", statusOut)
	}
}
