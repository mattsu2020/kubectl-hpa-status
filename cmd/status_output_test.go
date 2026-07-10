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

func TestRunStatusSingleEarlyOutputModesPreserveWarningExit(t *testing.T) {
	hpa := testutil.BuildHPA("default", "limited",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(1, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)

	tests := []struct {
		name   string
		mutate func(*options)
	}{
		{
			name: "structured",
			mutate: func(opts *options) {
				opts.Format = "structured"
			},
		},
		{
			name: "ai context",
			mutate: func(opts *options) {
				opts.ContextForAI = true
			},
		},
		{
			name: "gitops export",
			mutate: func(opts *options) {
				opts.Export = "yaml"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := &options{
				Common: commonOptions{ClientOverride: testutil.NewFakeClient(hpa)},
				Status: statusOptions{Features: feats("noEnrich")},
			}
			tc.mutate(opts)
			var out bytes.Buffer
			err := runStatusSingle(context.Background(), &out, opts, "limited", true)
			if !isExitCodeWarning(err) {
				t.Fatalf("error = %T %v, want warning exit", err, err)
			}
			if out.Len() == 0 {
				t.Fatal("expected output before warning exit")
			}
		})
	}
}
