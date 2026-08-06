package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/compare"
)

// compareTestOptions builds options backed by a fake client. compare resolves
// both sides through newCompareClient, which clones the options, so a single
// override serves as both the FROM and TO cluster; the two sides are
// distinguished by namespace instead.
func compareTestOptions(hpas ...*autoscalingv2.HorizontalPodAutoscaler) *options {
	return &options{
		Common: commonOptions{
			ConnectionOptions: ConnectionOptions{
				ClientOverride: testutil.NewFakeClient(hpas...),
				Namespace:      "prod",
			},
		},
	}
}

func TestRunCompareReportsSpecDifferences(t *testing.T) {
	staging := testutil.BuildHPA("staging", "web", testutil.WithMinMax(2, 20))
	prod := testutil.BuildHPA("prod", "web", testutil.WithMinMax(4, 8))
	opts := compareTestOptions(staging, prod)

	var out bytes.Buffer
	if err := runCompare(context.Background(), &out, opts, "staging/web", "prod/web", "", ""); err != nil {
		t.Fatalf("runCompare returned error: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"HPA Compare: staging/web -> prod/web",
		"Different:",
		"minReplicas: from=2 to=4",
		"maxReplicas: from=20 to=8",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	// A lower maxReplicas downstream is the risk this command exists to catch.
	if !strings.Contains(got, "Risk:") || !strings.Contains(got, "lower maxReplicas") {
		t.Errorf("expected a lower-maxReplicas risk, got:\n%s", got)
	}
}

func TestRunCompareIdenticalHPAsReportNoDifferences(t *testing.T) {
	a := testutil.BuildHPA("staging", "web", testutil.WithMinMax(2, 10))
	b := testutil.BuildHPA("prod", "web", testutil.WithMinMax(2, 10))
	opts := compareTestOptions(a, b)

	var out bytes.Buffer
	if err := runCompare(context.Background(), &out, opts, "staging/web", "prod/web", "", ""); err != nil {
		t.Fatalf("runCompare returned error: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Different:\n  none") {
		t.Errorf("expected no differences, got:\n%s", got)
	}
}

func TestRunCompareJSONOutput(t *testing.T) {
	staging := testutil.BuildHPA("staging", "web", testutil.WithMinMax(2, 20))
	prod := testutil.BuildHPA("prod", "web", testutil.WithMinMax(2, 8))
	opts := compareTestOptions(staging, prod)
	opts.Output = "json"

	var out bytes.Buffer
	if err := runCompare(context.Background(), &out, opts, "staging/web", "prod/web", "", ""); err != nil {
		t.Fatalf("runCompare returned error: %v", err)
	}

	var report compare.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("compare JSON is not decodable: %v\n%s", err, out.String())
	}
	if report.From != "staging/web" || report.To != "prod/web" {
		t.Errorf("unexpected from/to: %q -> %q", report.From, report.To)
	}
	if len(report.Differences) == 0 {
		t.Error("expected at least one difference in the structured report")
	}
}

func TestRunCompareMissingHPAFails(t *testing.T) {
	opts := compareTestOptions(testutil.BuildHPA("staging", "web"))

	var out bytes.Buffer
	err := runCompare(context.Background(), &out, opts, "staging/web", "prod/absent", "", "")
	if err == nil {
		t.Fatal("expected an error for a missing TO HPA")
	}
	if !strings.Contains(err.Error(), "fetching TO HPA") {
		t.Errorf("expected the error to name the TO side, got: %v", err)
	}
}

func TestRunCompareAllOnlyDriftFiltersMatchingHPAs(t *testing.T) {
	opts := compareTestOptions(testutil.BuildHPA("prod", "web"))
	opts.AllNamespaces = true

	// FROM and TO are the same fake cluster, so every HPA matches itself and
	// --only-drift must filter all of them out.
	var filtered bytes.Buffer
	if err := runCompareAll(context.Background(), &filtered, opts, "", "", true); err != nil {
		t.Fatalf("runCompareAll returned error: %v", err)
	}
	if got := filtered.String(); !strings.Contains(got, "No HPA drift found.") {
		t.Errorf("expected --only-drift to filter identical HPAs, got:\n%s", got)
	}

	// Without --only-drift every HPA is listed. The header still reads
	// "HPA drift" even when nothing differs, so assert the entry carries no
	// difference lines rather than asserting on the header wording.
	var unfiltered bytes.Buffer
	if err := runCompareAll(context.Background(), &unfiltered, opts, "", "", false); err != nil {
		t.Fatalf("runCompareAll returned error: %v", err)
	}
	got := unfiltered.String()
	if !strings.Contains(got, "prod/web -> prod/web") {
		t.Errorf("expected the matching HPA to be listed, got:\n%s", got)
	}
	if strings.Contains(got, "\n  - ") {
		t.Errorf("expected no difference lines for an HPA compared with itself, got:\n%s", got)
	}
}

func TestRunCompareAllReportsMissingHPAInTarget(t *testing.T) {
	// compare.BuildReport is only reached for HPAs present on both sides; the
	// "<missing>" entry is assembled directly by runCompareAll, so cover that
	// branch through the renderer contract it feeds.
	reports := []compare.Report{{
		From:        "prod/web",
		To:          "<missing>",
		Differences: []compare.Diff{{Field: "exists", From: "true", To: "false"}},
		Risks:       []string{"target environment is missing this HPA"},
	}}
	var out bytes.Buffer
	if err := renderCompareDriftText(&out, reports); err != nil {
		t.Fatalf("renderCompareDriftText returned error: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "<missing>") {
		t.Errorf("expected the missing-HPA marker, got:\n%s", got)
	}
}

func TestRenderCompareDriftTextEmptyAndPopulated(t *testing.T) {
	var empty bytes.Buffer
	if err := renderCompareDriftText(&empty, nil); err != nil {
		t.Fatalf("renderCompareDriftText returned error: %v", err)
	}
	if !strings.Contains(empty.String(), "No HPA drift found.") {
		t.Errorf("expected the empty-drift message, got: %q", empty.String())
	}

	var populated bytes.Buffer
	reports := []compare.Report{{
		From:        "prod/web",
		To:          "<missing>",
		Differences: []compare.Diff{{Field: "exists", From: "true", To: "false"}},
		Risks:       []string{"target environment is missing this HPA"},
	}}
	if err := renderCompareDriftText(&populated, reports); err != nil {
		t.Fatalf("renderCompareDriftText returned error: %v", err)
	}
	got := populated.String()
	for _, want := range []string{"HPA drift: prod/web -> <missing>", "exists: true -> false", "risk: target environment is missing this HPA"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in drift text, got:\n%s", want, got)
		}
	}
}

func TestSplitNamespacedRef(t *testing.T) {
	tests := []struct {
		ref              string
		defaultNamespace string
		wantNamespace    string
		wantName         string
	}{
		{"prod/web", "default", "prod", "web"},
		{"web", "default", "default", "web"},
		{"/web", "default", "", "web"},
	}
	for _, tc := range tests {
		namespace, name := splitNamespacedRef(tc.ref, tc.defaultNamespace)
		if namespace != tc.wantNamespace || name != tc.wantName {
			t.Errorf("splitNamespacedRef(%q, %q) = (%q, %q), want (%q, %q)",
				tc.ref, tc.defaultNamespace, namespace, name, tc.wantNamespace, tc.wantName)
		}
	}
}
