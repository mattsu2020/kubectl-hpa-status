//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	// Import the root command package from our plugin
	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// TestE2E_MultiNamespace verifies that list -A shows HPAs from multiple
// namespaces simultaneously.
func TestE2E_MultiNamespace(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName1 := setupTestNamespace(t, kubeconfig)
	_, client2, nsName2 := setupTestNamespace(t, kubeconfig)
	_ = client2

	createTestRC(t, client, nsName1, "multi-ns-rc-1")
	createHealthyHPA(t, client, nsName1, "multi-ns-hpa-1", "multi-ns-rc-1")

	createTestRC(t, client, nsName2, "multi-ns-rc-2")
	createHealthyHPA(t, client, nsName2, "multi-ns-hpa-2", "multi-ns-rc-2")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-A", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute list -A: %v. Output:\n%s", err, buf.String())
	}

	output := buf.String()
	t.Logf("Multi-namespace list output:\n%s", output)

	if !strings.Contains(output, "multi-ns-hpa-1") {
		t.Errorf("expected multi-ns-hpa-1 in output, got:\n%s", output)
	}
	if !strings.Contains(output, "multi-ns-hpa-2") {
		t.Errorf("expected multi-ns-hpa-2 in output, got:\n%s", output)
	}
}

// TestE2E_JSONStructuredOutput verifies that status -o json --explain
// produces valid JSON with structuredInterpretation, suggestions, and
// numeric healthScore fields.
func TestE2E_JSONStructuredOutput(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "json-struct-rc")
	createHealthyHPA(t, client, nsName, "json-struct-hpa", "json-struct-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "json-struct-hpa", "-n", nsName, "-o", "json", "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	raw := buf.String()
	t.Logf("JSON Structured Output:\n%s", raw)

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}

	analysis, ok := result["analysis"].(map[string]any)
	if !ok {
		t.Fatal("JSON output missing top-level 'analysis' object")
	}

	// Verify structuredInterpretation array is present.
	structuredInterp, ok := analysis["structuredInterpretation"].([]any)
	if !ok {
		t.Error("JSON analysis missing 'structuredInterpretation' array")
	} else if len(structuredInterp) == 0 {
		t.Error("JSON analysis 'structuredInterpretation' array is empty")
	}

	// Verify suggestions array exists.
	suggestions, ok := analysis["suggestions"].([]any)
	if !ok {
		t.Error("JSON analysis missing 'suggestions' array")
	} else {
		t.Logf("Found %d suggestions", len(suggestions))
	}

	// Verify healthScore is numeric.
	healthScore, ok := analysis["healthScore"].(float64)
	if !ok {
		t.Errorf("JSON analysis 'healthScore' is not numeric, got: %T", analysis["healthScore"])
	} else {
		t.Logf("Health score: %.0f", healthScore)
		if healthScore < 0 || healthScore > 100 {
			t.Errorf("healthScore %.0f is outside expected range [0, 100]", healthScore)
		}
	}
}

// TestE2E_ListApplyDryRun tests batch apply workflow with multiple HPAs.
func TestE2E_ListApplyDryRun(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	// Create two RC+HPA pairs
	createTestRC(t, client, nsName, "apply-rc-1")
	createTestRC(t, client, nsName, "apply-rc-2")

	// Create an HPA at maxReplicas (ScalingLimited scenario)
	createScalingLimitedHPA(t, client, nsName, "limited-hpa-1", "apply-rc-1")
	createScalingLimitedHPA(t, client, nsName, "limited-hpa-2", "apply-rc-2")

	// Test list --problem --apply (dry-run by default)
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-n", nsName, "--problem", "--apply", "--yes", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Logf("list --problem --apply returned error: %v. Output:\n%s", err, buf.String())
	}

	output := buf.String()
	t.Logf("List apply output:\n%s", output)

	// Should show dry-run validation or suggestion output
	if !strings.Contains(output, "limited-hpa-1") && !strings.Contains(output, "No applicable") {
		t.Errorf("expected limited-hpa-1 in output, got:\n%s", output)
	}
}

// TestE2E_MultipleProblems verifies that list --problem filters out healthy
// HPAs and keeps only those with ERROR/LIMITED classifications, while the
// unfiltered list shows every HPA in the namespace.
func TestE2E_MultipleProblems(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	// Three HPAs in one namespace: healthy, broken (ERROR), limited (LIMITED).
	createTestRC(t, client, nsName, "mp-healthy-rc")
	createHealthyHPA(t, client, nsName, "mp-healthy-hpa", "mp-healthy-rc")

	createTestRC(t, client, nsName, "mp-broken-rc")
	createBrokenHPA(t, client, nsName, "mp-broken-hpa", "mp-broken-rc")

	createTestRC(t, client, nsName, "mp-limited-rc")
	createScalingLimitedHPA(t, client, nsName, "mp-limited-hpa", "mp-limited-rc")

	// Unfiltered list must include all three HPAs.
	allBuf := new(bytes.Buffer)
	allCmd := cmd.NewRootCommand()
	allCmd.SetOut(allBuf)
	allCmd.SetErr(allBuf)
	allCmd.SetArgs([]string{"list", "-n", nsName, "--kubeconfig", kubeconfig})

	if err := allCmd.Execute(); err != nil {
		t.Fatalf("failed to execute list: %v. Output:\n%s", err, allBuf.String())
	}

	allOutput := allBuf.String()
	t.Logf("Multiple problems unfiltered list:\n%s", allOutput)
	for _, name := range []string{"mp-healthy-hpa", "mp-broken-hpa", "mp-limited-hpa"} {
		if !strings.Contains(allOutput, name) {
			t.Errorf("expected %q in unfiltered list output, got:\n%s", name, allOutput)
		}
	}

	// --problem must keep broken and limited HPAs while excluding the healthy one.
	probBuf := new(bytes.Buffer)
	probCmd := cmd.NewRootCommand()
	probCmd.SetOut(probBuf)
	probCmd.SetErr(probBuf)
	probCmd.SetArgs([]string{"list", "-n", nsName, "--problem", "--kubeconfig", kubeconfig})

	if err := probCmd.Execute(); err != nil {
		t.Fatalf("failed to execute list --problem: %v. Output:\n%s", err, probBuf.String())
	}

	probOutput := probBuf.String()
	t.Logf("Multiple problems --problem list:\n%s", probOutput)

	if !strings.Contains(probOutput, "mp-broken-hpa") {
		t.Errorf("expected mp-broken-hpa in --problem output, got:\n%s", probOutput)
	}
	if !strings.Contains(probOutput, "mp-limited-hpa") {
		t.Errorf("expected mp-limited-hpa in --problem output, got:\n%s", probOutput)
	}
	if strings.Contains(probOutput, "mp-healthy-hpa") {
		t.Errorf("healthy HPA mp-healthy-hpa must NOT appear in --problem output, got:\n%s", probOutput)
	}
}
