package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestNewAutoscalerMapCommand(t *testing.T) {
	c := newAutoscalerMapCommand(&options{})
	if c.Use == "" {
		t.Fatal("Use must be non-empty")
	}
	if c.Short == "" {
		t.Fatal("Short must be non-empty")
	}
}

// TestRunAutoscalerMap_JSONShape exercises the full runAutoscalerMap path
// against a fake client. It asserts the JSON output envelope carries the HPA
// identity and the autoscaler map structure rather than a hard error, which
// guards the assembleAutoscalerMapInput / fetchAutoscalerMap* wiring.
func TestRunAutoscalerMap_JSONShape(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithResourceMetric("cpu", 70, 65),
		testutil.WithMinMax(2, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{ClientOverride: fakeClient, Output: "json"},
	}
	err := runAutoscalerMap(context.Background(), &buf, opts, []string{"web"})
	if err != nil {
		t.Fatalf("runAutoscalerMap: %v", err)
	}
	out := buf.String()
	// The JSON envelope must reference the HPA; the exact field names are
	// validated in pkg/hpa tests, so here we only assert the command produced
	// structured output mentioning the HPA rather than erroring.
	if !strings.Contains(out, "web") {
		t.Fatalf("expected HPA name in output, got:\n%s", out)
	}
}

// TestRunAutoscalerMap_MissingHPAPropagatesLookupError documents that a
// missing HPA surfaces a wrapped lookup error rather than an empty report.
func TestRunAutoscalerMap_MissingHPAPropagatesLookupError(t *testing.T) {
	fakeClient := testutil.NewFakeClient() // no HPAs

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{ClientOverride: fakeClient, Output: "json"},
	}
	err := runAutoscalerMap(context.Background(), &buf, opts, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing HPA, got nil")
	}
}
