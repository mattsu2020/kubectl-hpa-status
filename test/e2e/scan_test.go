//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// TestE2E_ScanSurfacesProblemsClusterWide exercises the `scan` command
// against multiple namespaces (Issue #10 gap: the existing scan coverage in
// e2e_status_test.go only ran scan against a single healthy HPA and asserted
// the command didn't error, never that it actually surfaced a problem).
//
// scan is list -A --problem under the hood (see
// internal/cmdoptions.NewScanRequest), so it must report broken/limited HPAs
// by name across namespaces while excluding healthy ones from its output.
func TestE2E_ScanSurfacesProblemsClusterWide(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	_, healthyClient, healthyNs := setupTestNamespace(t, kubeconfig)
	_, problemClient, problemNs := setupTestNamespace(t, kubeconfig)

	createTestRC(t, healthyClient, healthyNs, "scan-healthy-rc")
	createHealthyHPA(t, healthyClient, healthyNs, "scan-healthy-hpa", "scan-healthy-rc")

	createTestRC(t, problemClient, problemNs, "scan-broken-rc")
	createBrokenHPA(t, problemClient, problemNs, "scan-broken-hpa", "scan-broken-rc")

	createTestRC(t, problemClient, problemNs, "scan-limited-rc")
	createScalingLimitedHPA(t, problemClient, problemNs, "scan-limited-hpa", "scan-limited-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"scan", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute scan command: %v. Output:\n%s", err, buf.String())
	}

	output := buf.String()
	t.Logf("Cluster-wide scan output:\n%s", output)

	if !strings.Contains(output, "scan-broken-hpa") {
		t.Errorf("expected scan-broken-hpa in cluster-wide scan output, got:\n%s", output)
	}
	if !strings.Contains(output, "scan-limited-hpa") {
		t.Errorf("expected scan-limited-hpa in cluster-wide scan output, got:\n%s", output)
	}
	if strings.Contains(output, "scan-healthy-hpa") {
		t.Errorf("healthy HPA scan-healthy-hpa must NOT appear in scan output, got:\n%s", output)
	}
}
