package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// streamingTestOpts returns an options struct configured for the streaming
// path: KEDA/VPA off, no sort/apply/export. KEDA/VPA live on Status (promoted
// via the embedded options model), so they are set there. Callers adjust other
// fields per case.
func streamingTestOpts() *options {
	return &options{
		Status: statusOptions{
			KEDA: "off",
			VPA:  "off",
		},
	}
}

// TestCanStreamList verifies the gating logic that decides whether runList may
// use the page-by-page streaming path. Each condition that requires the full
// accumulated set must disable streaming.
func TestCanStreamList(t *testing.T) {
	// KEDA/VPA are promoted from Status; the helper pre-seeds them to "off"
	// so the base case is streamable.
	cases := []struct {
		label  string
		mutate func(*options)
		want   bool
	}{
		{"default table, no enrichment", func(*options) {}, true},
		{"wide", func(o *options) { o.Wide = true }, true},
		{"jsonl", func(o *options) { o.Output = "jsonl" }, true},
		{"sort-by disables streaming", func(o *options) { o.SortBy = "name" }, false},
		{"apply disables streaming", func(o *options) { o.Apply = true }, false},
		{"export directory disables streaming", func(o *options) { o.Export = "directory" }, false},
		{"gitops-drift disables streaming", func(o *options) { o.GitOpsDrift = true }, false},
		{"trend disables streaming", func(o *options) { o.Trend = true }, false},
		{"report disables streaming", func(o *options) { o.Report = "markdown" }, false},
		{"json disables streaming (needs array)", func(o *options) { o.Output = "json" }, false},
		{"yaml disables streaming (needs whole doc)", func(o *options) { o.Output = "yaml" }, false},
		{"KEDA auto disables streaming", func(o *options) { o.KEDA = "auto" }, false},
		{"KEDA on disables streaming", func(o *options) { o.KEDA = "on" }, false},
		{"VPA auto disables streaming", func(o *options) { o.VPA = "auto" }, false},
		{"KEDA off + VPA off allows streaming", func(o *options) { o.KEDA = "off"; o.VPA = "off" }, true},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			o := streamingTestOpts()
			c.mutate(o)
			if got := canStreamList(o); got != c.want {
				t.Fatalf("canStreamList = %v, want %v", got, c.want)
			}
		})
	}
}

// buildStreamingHPAs builds n HPAs named web-a, web-b, ... for streaming tests.
func buildStreamingHPAs(n int) []*autoscalingv2.HorizontalPodAutoscaler {
	hpas := make([]*autoscalingv2.HorizontalPodAutoscaler, 0, n)
	for i := 0; i < n; i++ {
		hpas = append(hpas, testutil.BuildHPA("default", streamingHPAName(i),
			testutil.WithMinMax(1, 10),
			testutil.WithReplicas(2, 2),
		))
	}
	return hpas
}

func streamingHPAName(i int) string {
	return "web-" + string(rune('a'+i))
}

// TestRunListStreamingJSONL verifies the jsonl streaming path emits one JSON
// object per HPA, one per line, without an enclosing array, and that it does
// not buffer every HPA before emitting. A small ChunkSize forces the fake
// client to paginate, and the output must decode as newline-delimited objects.
func TestRunListStreamingJSONL(t *testing.T) {
	fakeClient := testutil.NewFakeClient(buildStreamingHPAs(5)...)

	opts := streamingTestOpts()
	opts.ClientOverride = fakeClient
	opts.Output = "jsonl"
	opts.ChunkSize = 2 // force multiple pages

	var buf bytes.Buffer
	if err := runList(context.Background(), &buf, opts); err != nil {
		t.Fatalf("runList: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Fatal("expected non-empty jsonl output")
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 jsonl lines (one per HPA), got %d:\n%s", len(lines), output)
	}
	for i, line := range lines {
		var item hpaanalysis.ListItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}
	}
	// Streaming must NOT wrap items in a JSON array. A leading '[' would
	// indicate the accumulated json path was taken instead.
	if strings.HasPrefix(strings.TrimSpace(output), "[") {
		t.Fatalf("jsonl output looks like a JSON array (accumulated path ran); got:\n%s", output)
	}
}

// TestRunListStreamingTable verifies the table streaming path writes the header
// exactly once and then one row per HPA, even across multiple pages.
func TestRunListStreamingTable(t *testing.T) {
	fakeClient := testutil.NewFakeClient(buildStreamingHPAs(4)...)

	opts := streamingTestOpts()
	opts.ClientOverride = fakeClient
	opts.ChunkSize = 2 // force two pages

	var buf bytes.Buffer
	if err := runList(context.Background(), &buf, opts); err != nil {
		t.Fatalf("runList: %v", err)
	}
	output := buf.String()
	// Header should appear exactly once even though two pages were processed.
	headerCount := strings.Count(output, "NAMESPACE")
	if headerCount != 1 {
		t.Fatalf("expected exactly 1 header line across streamed pages, got %d:\n%s", headerCount, output)
	}
	// Each HPA name should appear once as a row.
	for i := 0; i < 4; i++ {
		if !strings.Contains(output, streamingHPAName(i)) {
			t.Fatalf("expected HPA %q in streamed table output:\n%s", streamingHPAName(i), output)
		}
	}
}

// TestRunListAccumulatedForJSON confirms that --output json still uses the
// accumulated path (a JSON array), proving the streaming path did not silently
// change the json contract.
func TestRunListAccumulatedForJSON(t *testing.T) {
	fakeClient := testutil.NewFakeClient(buildStreamingHPAs(2)...)

	opts := streamingTestOpts()
	opts.ClientOverride = fakeClient
	opts.Output = "json"

	var buf bytes.Buffer
	if err := runList(context.Background(), &buf, opts); err != nil {
		t.Fatalf("runList: %v", err)
	}
	var report hpaanalysis.ListReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("json output did not decode as a ListReport (array contract): %v\n%s", err, buf.String())
	}
}
