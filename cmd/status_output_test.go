package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestRunStatusMultipleKeepsMarkdownStdoutCleanOnItemFailure(t *testing.T) {
	fakeClient := testutil.NewFakeClient(testutil.BuildHPA("default", "web"))
	var stdout, stderr bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "markdown",
			Err:            &stderr,
		},
		Status: statusOptions{
			KEDA: "off",
			VPA:  "off",
		},
	}

	err := runStatusMany(context.Background(), &stdout, opts, []string{"web", "missing"}, false)
	if err == nil {
		t.Fatal("runStatusMany returned nil, want aggregate item error")
	}
	if strings.Contains(stdout.String(), "missing") {
		t.Fatalf("markdown stdout contains an item error:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "# HPA Status Report") {
		t.Fatalf("markdown stdout does not contain the successful report:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), `HPA "missing"`) {
		t.Fatalf("stderr does not contain the failed item:\n%s", stderr.String())
	}
}
