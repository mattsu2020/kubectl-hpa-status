package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// TestNormalizeOutputFlagCanonicalizesCasing pins the fix for the
// validation/render disagreement: --output=JSON used to pass validation
// (which normalized for the check) and then fail at render time after all
// API work was done. Normalization now happens once, on opts.
func TestNormalizeOutputFlagCanonicalizesCasing(t *testing.T) {
	cases := map[string]string{
		"JSON":        "json",
		"  YAML ":     "yaml",
		"Jsonl":       "jsonl",
		"gotemplate":  "go-template",
		"table":       "table",
		"":            "",
		"unknown-foo": "unknown-foo",
	}
	for in, want := range cases {
		if got := normalizeOutputFlag(in); got != want {
			t.Errorf("normalizeOutputFlag(%q) = %q, want %q", in, got, want)
		}
	}
	// Expressions keep their case; only the format prefix is canonicalized.
	if got := normalizeOutputFlag("JSONPATH={.items[*].metadata.name}"); got != "jsonpath={.items[*].metadata.name}" {
		t.Errorf("jsonpath expression casing not preserved: %q", got)
	}
	if got := normalizeOutputFlag("Template={{.Name}}"); got != "template={{.Name}}" {
		t.Errorf("template expression casing not preserved: %q", got)
	}
}

// TestValidateEffectiveOptionsNormalizesOutput verifies the end-to-end
// behavior: after validation, opts.Output holds the canonical value the
// render dispatcher expects.
func TestValidateEffectiveOptionsNormalizesOutput(t *testing.T) {
	opts := defaultRootOptions()
	opts.Concurrency = defaultConcurrency()
	opts.WatchInterval = defaultPollInterval
	opts.Output = "JSON"
	if err := validateEffectiveOptions(nil, &opts); err != nil {
		t.Fatalf("validateEffectiveOptions(JSON) error: %v", err)
	}
	if opts.Output != "json" {
		t.Fatalf("opts.Output = %q, want canonical %q", opts.Output, "json")
	}
}

// TestWriteConflictScanReportSupportsJSONPath pins the conflicts-scan fix:
// structured formats route through internal/render, so jsonpath (which
// validation already accepted) executes instead of silently printing text.
func TestWriteConflictScanReportSupportsJSONPath(t *testing.T) {
	report := conflictScanReport{
		Items: []conflictItem{{
			Namespace: "default",
			Target:    "Deployment/web",
			Risks:     []string{"two HPAs target the same scale target"},
		}},
	}
	opts := &options{}
	opts.Output = "jsonpath={.items[0].namespace}"

	var buf bytes.Buffer
	if err := writeConflictScanReport(&buf, opts, report); err != nil {
		t.Fatalf("writeConflictScanReport jsonpath error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "default" {
		t.Fatalf("jsonpath output = %q, want %q", got, "default")
	}
}

// TestBuildGitOpsConflictWarnsOnUnsupportedKind pins the fix for the missing
// default branch: a scale target kind outside Deployment/StatefulSet now
// produces a live-fetch warning instead of a silent liveReplicas=0 drift
// analysis.
func TestBuildGitOpsConflictWarnsOnUnsupportedKind(t *testing.T) {
	hpa := testutil.BuildHPA("default", "daemon-hpa",
		testutil.WithScaleTargetRef("DaemonSet", "logs"),
	)
	client := &kube.Client{Interface: testutil.NewFakeClient(), Namespace: "default"}

	conflict, _ := buildGitOpsConflict(context.Background(), client, hpa, "")
	found := false
	for _, w := range conflict.Warnings {
		if strings.Contains(w, "DaemonSet") && strings.Contains(w, "logs") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported-kind warning in %v", conflict.Warnings)
	}
}

// TestWriteGitOpsExportReportsNoPatch ensures the directory export's skip
// decision uses the returned flag instead of substring-matching rendered
// English output.
func TestWriteGitOpsExportReportsNoPatch(t *testing.T) {
	report := hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{
		Meta: hpaanalysis.MetaView{Namespace: "default", Name: "web"},
	}}
	var buf bytes.Buffer
	exported, err := writeGitOpsExport(&buf, "yaml", report)
	if err != nil {
		t.Fatalf("writeGitOpsExport error: %v", err)
	}
	if exported {
		t.Fatal("expected exported=false when no suggestions apply")
	}

	report.Analysis.Actions.Suggestions = []hpaanalysis.Suggestion{{
		Title: "raise maxReplicas",
		Apply: true,
		Patch: `{"spec":{"maxReplicas":10}}`,
	}}
	var buf2 bytes.Buffer
	exported2, err := writeGitOpsExport(&buf2, "yaml", report)
	if err != nil {
		t.Fatalf("writeGitOpsExport error: %v", err)
	}
	if !exported2 {
		t.Fatal("expected exported=true when a patch applies")
	}
}

// TestClampPollIntervalShared pins the shared clamp used by watch, timeline,
// and record: sub-second intervals clamp up with a warning; healthy
// intervals pass through untouched.
func TestClampPollIntervalShared(t *testing.T) {
	var buf bytes.Buffer
	got, err := clampPollInterval(&buf, 100*1000*1000) // 100ms
	if err != nil {
		t.Fatalf("clampPollInterval error: %v", err)
	}
	if got != minWatchInterval {
		t.Fatalf("interval = %v, want %v", got, minWatchInterval)
	}
	if !strings.Contains(buf.String(), "clamping to 1s") {
		t.Fatalf("expected clamp warning, got %q", buf.String())
	}

	var buf2 bytes.Buffer
	got2, err := clampPollInterval(&buf2, 5*1000*1000*1000) // 5s
	if err != nil {
		t.Fatalf("clampPollInterval error: %v", err)
	}
	if got2 != 5*1000*1000*1000 {
		t.Fatalf("healthy interval must pass through, got %v", got2)
	}
	if buf2.Len() != 0 {
		t.Fatalf("healthy interval must not warn, got %q", buf2.String())
	}
}
