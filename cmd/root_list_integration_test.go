package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestRunList_MultipleHPAs(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(2, 2),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Errorf("expected 'web' in list output, got:\n%s", output)
	}
	if !strings.Contains(output, "api") {
		t.Errorf("expected 'api' in list output, got:\n%s", output)
	}
}

func TestRunList_Filter(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(2, 2),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			Filter: "error",
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "web") {
		t.Errorf("expected 'web' to be filtered out, got:\n%s", output)
	}
	if !strings.Contains(output, "api") {
		t.Errorf("expected 'api' in filtered output, got:\n%s", output)
	}
}

func TestRunListProblemFiltersVisibleIssues(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(2, 2),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			Problem: true,
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "web") {
		t.Errorf("expected healthy HPA to be filtered out, got:\n%s", output)
	}
	if !strings.Contains(output, "api") {
		t.Errorf("expected problematic HPA in output, got:\n%s", output)
	}
}

func TestRunListHealthScoreThresholdFiltersByScore(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(2, 2),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			HealthScoreMax: 80,
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "web") {
		t.Errorf("expected healthy HPA to be filtered out, got:\n%s", output)
	}
	if !strings.Contains(output, "api") {
		t.Errorf("expected low-score HPA in output, got:\n%s", output)
	}
}

func TestRunList_SortByDesired(t *testing.T) {
	smallHPA := testutil.BuildHPA("default", "small", testutil.WithReplicas(1, 2))
	largeHPA := testutil.BuildHPA("default", "large", testutil.WithReplicas(5, 10))
	fakeClient := testutil.NewFakeClient(largeHPA, smallHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			SortBy: "desired",
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	smallIdx := strings.Index(output, "small")
	largeIdx := strings.Index(output, "large")
	if smallIdx == -1 || largeIdx == -1 {
		t.Fatalf("expected both HPAs in output, got:\n%s", output)
	}
	if smallIdx > largeIdx {
		t.Errorf("expected 'small' (desired=2) before 'large' (desired=10), got:\n%s", output)
	}
}

func TestRunList_Wide(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithMinMax(2, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			Wide:           true,
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	for _, col := range []string{"TARGET", "MIN", "MAX"} {
		if !strings.Contains(output, col) {
			t.Errorf("expected %s column in wide output, got:\n%s", col, output)
		}
	}
}

func TestRunList_LabelSelector(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	webHPA.Labels = map[string]string{"app": "web", "tier": "frontend"}
	apiHPA := testutil.BuildHPA("default", "api", testutil.WithReplicas(2, 2))
	apiHPA.Labels = map[string]string{"app": "api", "tier": "backend"}
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			Selector:       "app=web",
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Fatalf("expected selected HPA in output, got:\n%s", output)
	}
	if strings.Contains(output, "api") {
		t.Fatalf("expected api to be filtered out by selector, got:\n%s", output)
	}
}

func TestRunListApplyBatchSummaryAndConfirmation(t *testing.T) {
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	webHPA := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(apiHPA, webHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			In:             io.Reader(strings.NewReader("")),
			Apply:          true,
			DryRun:         true,
			Yes:            true,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			Problem: true,
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Batch patch summary") {
		t.Fatalf("expected batch patch summary header, got:\n%s", output)
	}
	if !strings.Contains(output, "Batch complete:") {
		t.Fatalf("expected batch complete summary, got:\n%s", output)
	}
	if !strings.Contains(output, "2 succeeded") {
		t.Fatalf("expected 2 succeeded, got:\n%s", output)
	}
}

func TestRunListApplyBatchSkippedOnNoInput(t *testing.T) {
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			In:             io.Reader(strings.NewReader("n\n")),
			Apply:          true,
			DryRun:         true,
			Yes:            false,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			Problem: true,
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Batch apply skipped") {
		t.Fatalf("expected batch apply skipped message, got:\n%s", output)
	}
}

func TestRunListApplyBatchNoPatchesFound(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			In:             io.Reader(strings.NewReader("")),
			Apply:          true,
			DryRun:         true,
			Yes:            true,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		List: listOptions{
			HealthScoreMax: 80,
		},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Watch command integration tests
// --------------------------------------------------------------------------
