//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file is the shared home for the JSON "schema snapshot" style of E2E
// assertion (#8 in the E2E-expansion roadmap). Instead of golden files, the
// output is decoded into the typed hpaanalysis.StatusReport and the stable
// shape of the report is validated. Timestamps and other non-deterministic
// values are intentionally not asserted.
//
// Helpers here are reused by tests in sibling files (hpa_realbehavior_test.go,
// conflict_apply_test.go, and the augmented tests in e2e_test.go).

// validHealthStates is the closed set of health states the analysis model
// can emit. Any value outside this set means the schema contract is broken.
var validHealthStates = map[string]bool{
	"OK":         true,
	"ERROR":      true,
	"LIMITED":    true,
	"STABILIZED": true,
}

// runStatusJSON executes `status <hpa> -o json` with the given extra args and
// returns the raw JSON output. It fatals on execution errors that are not
// ExitCodeError (which is expected for ERROR/LIMITED health).
func runStatusJSON(t *testing.T, kubeconfig, namespace, hpaName string, extraArgs ...string) string {
	t.Helper()
	args := []string{"status", hpaName, "-n", namespace, "-o", "json"}
	args = append(args, extraArgs...)
	args = append(args, "--kubeconfig", kubeconfig)

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)

	if err := rootCmd.Execute(); err != nil {
		// ExitCodeError is the expected non-zero exit for ERROR/LIMITED HPAs.
		var exitErr *cmd.ExitCodeError
		if !asExitCodeError(err, &exitErr) {
			t.Fatalf("status -o json failed: %v. Output:\n%s", err, buf.String())
		}
	}
	return buf.String()
}

// asExitCodeError mirrors errors.As but keeps the helper self-contained.
func asExitCodeError(err error, target **cmd.ExitCodeError) bool {
	for e := err; e != nil; {
		if ec, ok := e.(*cmd.ExitCodeError); ok {
			*target = ec
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := e.(unwrapper); ok {
			e = u.Unwrap()
			continue
		}
		return false
	}
	return false
}

// decodeStatusReport unmarshals raw JSON into a typed StatusReport. It fatals
// on decode errors so callers can assume a valid report afterwards.
func decodeStatusReport(t *testing.T, raw string) hpaanalysis.StatusReport {
	t.Helper()
	var report hpaanalysis.StatusReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("output does not decode into hpaanalysis.StatusReport: %v\nraw:\n%s", err, raw)
	}
	return report
}

// assertStatusReportShape validates the stable, deterministic shape of a
// StatusReport. It checks the schema version, identity fields, the closed
// health-state set, the health-score range, and the presence of the core
// always-present arrays (conditions, metrics). It deliberately does NOT
// assert on timestamps, replica counts, or metric ratios, which depend on
// live cluster state.
func assertStatusReportShape(t *testing.T, raw string, wantName string) {
	t.Helper()
	report := decodeStatusReport(t, raw)
	a := report.Analysis

	// Schema version is the public output contract. Bumping it is a breaking
	// change, so any drift here should fail loudly.
	if report.APIVersion != hpaanalysis.SchemaVersion {
		t.Errorf("apiVersion = %q, want %q", report.APIVersion, hpaanalysis.SchemaVersion)
	}
	if !strings.HasSuffix(report.APIVersion, "/v1") {
		t.Errorf("apiVersion %q does not end with the expected /v1 suffix", report.APIVersion)
	}

	// Identity.
	if a.Name == "" {
		t.Error("Analysis.Name is empty")
	}
	if wantName != "" && a.Name != wantName {
		t.Errorf("Analysis.Name = %q, want %q", a.Name, wantName)
	}
	if a.Namespace == "" {
		t.Error("Analysis.Namespace is empty")
	}
	if a.Target == "" {
		t.Error("Analysis.Target (scaleTargetRef) is empty")
	}

	// Health state must be in the closed known set.
	if !validHealthStates[a.Health] {
		t.Errorf("Analysis.Health = %q, not a known state %v", a.Health, validHealthStates)
	}

	// Health score is clamped to [0, 100] by the model.
	if a.HealthScore < 0 || a.HealthScore > 100 {
		t.Errorf("Analysis.HealthScore = %d, want range [0, 100]", a.HealthScore)
	}

	// Summary is always populated by the analyzer.
	if a.Summary == "" {
		t.Error("Analysis.Summary is empty")
	}

	// Conditions and metrics slices must be present (possibly empty for
	// metrics on a broken HPA, but the slice itself must round-trip).
	if a.Conditions == nil {
		t.Error("Analysis.Conditions is nil; expected an initialized slice")
	}
	if len(a.Conditions) == 0 {
		t.Error("Analysis.Conditions is empty; every HPA has at least one condition")
	}
	if a.Metrics == nil {
		t.Error("Analysis.Metrics is nil; expected an initialized slice")
	}

	// Each condition must carry a Type and Status at minimum.
	for i, c := range a.Conditions {
		if c.Type == "" {
			t.Errorf("Conditions[%d].Type is empty", i)
		}
		if c.Status == "" {
			t.Errorf("Conditions[%d].Status is empty for %q", i, c.Type)
		}
	}

	// Each metric must carry a Type and non-empty Text.
	for i, m := range a.Metrics {
		if m.Type == "" {
			t.Errorf("Metrics[%d].Type is empty", i)
		}
		if m.Text == "" {
			t.Errorf("Metrics[%d].Text is empty", i)
		}
	}
}

// decodeStatusReportJSON mirrors decodeStatusReport but returns the loose
// map form for tests that need to probe raw keys not exposed on the typed
// struct (e.g. additive fields added by enrichment flags).
func decodeStatusReportJSON(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}
	return result
}

// analysisMap extracts the nested "analysis" object from a decoded JSON map.
func analysisMap(t *testing.T, result map[string]interface{}) map[string]interface{} {
	t.Helper()
	a, ok := result["analysis"].(map[string]interface{})
	if !ok {
		t.Fatalf("JSON output missing top-level 'analysis' object; got keys: %v", keysOf(result))
	}
	return a
}

// keysOf returns the keys of m in a stable form for diagnostic logging.
func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestE2E_JSONShapeSnapshot is the schema-snapshot test (#8). It runs a
// healthy HPA through `status -o json --explain` and asserts that every
// always-present field of the StatusReport schema is present and well-typed.
// This is a stronger contract check than TestE2E_JSONTypedDecode: it
// validates the full stable shape rather than a few scalar values, so any
// additive-only schema drift in a future change is caught here.
func TestE2E_JSONShapeSnapshot(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "shape-rc")
	createHealthyHPA(t, client, nsName, "shape-hpa", "shape-rc")

	raw := runStatusJSON(t, kubeconfig, nsName, "shape-hpa", "--explain")
	t.Logf("Shape snapshot JSON:\n%s", raw)

	// 1. Typed decode + full shape assertion.
	assertStatusReportShape(t, raw, "shape-hpa")

	// 2. Loose map assertions for fields the test wants to inspect directly.
	result := decodeStatusReportJSON(t, raw)
	a := analysisMap(t, result)

	// apiVersion is a top-level field, not under analysis.
	if got, _ := result["apiVersion"].(string); got != hpaanalysis.SchemaVersion {
		t.Errorf("top-level apiVersion = %q, want %q", got, hpaanalysis.SchemaVersion)
	}

	// --explain populates structuredInterpretation (additive field).
	structInterp, ok := a["structuredInterpretation"].([]interface{})
	if !ok {
		t.Error("analysis.structuredInterpretation missing or wrong type after --explain")
	} else if len(structInterp) == 0 {
		t.Error("analysis.structuredInterpretation is empty after --explain")
	}

	// suggestions is always an array (possibly empty).
	if _, ok := a["suggestions"].([]interface{}); !ok {
		t.Errorf("analysis.suggestions missing or wrong type; got %T", a["suggestions"])
	}

	// events is optional (omitempty), but when present must be an array.
	if ev, present := result["events"]; present {
		if _, ok := ev.([]interface{}); !ok {
			t.Errorf("top-level events present but wrong type; got %T", ev)
		}
	}

	// 3. Replica fields round-trip as numbers (decoded as float64 by encoding/json).
	for _, key := range []string{"currentReplicas", "desiredReplicas", "minReplicas", "maxReplicas"} {
		v, ok := a[key].(float64)
		if !ok {
			t.Errorf("analysis.%s missing or not numeric; got %T", key, a[key])
			continue
		}
		if v < 0 {
			t.Errorf("analysis.%s = %v, must be non-negative", key, v)
		}
	}
}
