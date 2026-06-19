package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// --------------------------------------------------------------------------
// Status command integration tests
// --------------------------------------------------------------------------

func TestRunStatus_OK(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: true, Limit: 5},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "web", true)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected HPA header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "current=3 desired=5") {
		t.Errorf("expected replica info in output, got:\n%s", output)
	}
	if !strings.Contains(output, "scale up") {
		t.Errorf("expected scale up summary, got:\n%s", output)
	}
}

func TestRunStatus_ScalingLimited(t *testing.T) {
	hpa := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	if err == nil {
		t.Fatal("expected ExitCodeError for ScalingLimited, got nil")
	}
	exitErr, ok := err.(*ExitCodeError)
	if !ok {
		t.Fatalf("expected *ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitWarning {
		t.Fatalf("expected exit code %d, got %d", ExitWarning, exitErr.Code)
	}
	output := buf.String()
	if !strings.Contains(output, "maxReplicas") {
		t.Errorf("expected maxReplicas mention in output, got:\n%s", output)
	}
	if !strings.Contains(output, "ScalingLimited") {
		t.Errorf("expected ScalingLimited condition in output, got:\n%s", output)
	}
}

func TestRunStatusSuggestShowsPatchCommand(t *testing.T) {
	hpa := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				Suggest: true,
			},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "kubectl patch hpa api") {
		t.Fatalf("expected patch command in suggest output, got:\n%s", output)
	}
}

func TestRunStatusApplyPatchesHPA(t *testing.T) {
	hpa := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			In:             io.Reader(strings.NewReader("")),
			Apply:          true,
			DryRun:         false,
			Yes:            true,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	got, err := fakeClient.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.MaxReplicas != 20 {
		t.Fatalf("expected maxReplicas=20 after apply, got %d", got.Spec.MaxReplicas)
	}
}

func TestRunStatusApplyDefaultsToDryRun(t *testing.T) {
	hpa := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
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
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Dry-run mode is enabled") {
		t.Fatalf("expected dry-run warning, got:\n%s", output)
	}
	if !strings.Contains(output, "spec.maxReplicas: 10 -> 20") {
		t.Fatalf("expected diff output, got:\n%s", output)
	}
}

func TestRunStatus_MetricsFetchFailure(t *testing.T) {
	hpa := testutil.BuildHPA("default", "broken",
		testutil.WithReplicas(2, 0),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "broken", true)
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "FailedGetResourceMetric") {
		t.Errorf("expected FailedGetResourceMetric in output, got:\n%s", output)
	}
	if !strings.Contains(output, "cannot currently compute") {
		t.Errorf("expected cannot-compute summary, got:\n%s", output)
	}
}

func TestRunStatus_NotFound(t *testing.T) {
	fakeClient := testutil.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent HPA, got nil")
	}
	if !errors.Is(err, ErrHPANotFound) {
		t.Errorf("expected ErrHPANotFound, got: %v", err)
	}
}

func TestRunStatus_JSONOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			Output:         "json",
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if report.Analysis.Name != "web" {
		t.Errorf("expected analysis.name=web, got %s", report.Analysis.Name)
	}
	if report.Analysis.Current != 3 {
		t.Errorf("expected current=3, got %d", report.Analysis.Current)
	}
}

func TestRunStatusMany_TextOutput(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	apiHPA := testutil.BuildHPA("default", "api", testutil.WithReplicas(2, 2))
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
	err := runStatusMany(context.Background(), &buf, opts, []string{"web", "api"}, false)
	if err != nil {
		t.Fatalf("runStatusMany returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA default/web") || !strings.Contains(output, "HPA default/api") {
		t.Fatalf("expected both HPAs in output, got:\n%s", output)
	}
}

func TestRunStatusMany_JSONOutput(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	apiHPA := testutil.BuildHPA("default", "api", testutil.WithReplicas(2, 2))
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			Output:         "json",
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatusMany(context.Background(), &buf, opts, []string{"web", "api"}, false)
	if err != nil {
		t.Fatalf("runStatusMany returned error: %v", err)
	}

	var reports []hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &reports); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if len(reports) != 2 || reports[0].Analysis.Name != "web" || reports[1].Analysis.Name != "api" {
		t.Fatalf("unexpected reports: %#v", reports)
	}
}

func TestRunStatus_YAMLOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			Output:         "yaml",
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "name: web") {
		t.Errorf("expected name: web in YAML output, got:\n%s", output)
	}
	if !strings.Contains(output, "currentReplicas: 3") {
		t.Errorf("expected currentReplicas: 3 in YAML output, got:\n%s", output)
	}
}

func TestRunStatus_WithEvents(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	ev1 := testutil.BuildEvent("default", "web", "SuccessfulRescale", "New size: 5")
	ev2 := testutil.BuildEvent("default", "web", "DesiredReplicasComputed", "calculated 5")
	fakeClient := testutil.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1, ev2},
	)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: true, Limit: 5},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "SuccessfulRescale") {
		t.Errorf("expected SuccessfulRescale event in output, got:\n%s", output)
	}
}

// --------------------------------------------------------------------------
// List command integration tests
// --------------------------------------------------------------------------

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

func TestRunWatch_TimeoutExpires(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 3))
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		Watch: watchOptions{
			WatchInterval: 100 * time.Millisecond,
			WatchTimeout:  250 * time.Millisecond,
		},
	}
	err := runWatch(context.Background(), &buf, opts, "web", false)
	if err == nil {
		t.Fatal("expected context deadline exceeded error, got nil")
	}
	output := buf.String()
	if !strings.Contains(output, "Updated:") {
		t.Errorf("expected at least one watch update, got:\n%s", output)
	}
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected HPA header in watch output, got:\n%s", output)
	}
}

func TestRunWatch_UntilCondition(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
		Watch: watchOptions{
			WatchInterval:  100 * time.Millisecond,
			UntilCondition: "scaling-limited",
		},
	}
	err := runWatch(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runWatch returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Stopped") {
		t.Errorf("expected 'Stopped' message when condition found, got:\n%s", output)
	}
}

// --------------------------------------------------------------------------
// Exit code integration tests
// --------------------------------------------------------------------------

func TestRunStatus_ExitCode_HealthyHPA(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "web", true)
	if err != nil {
		t.Fatalf("expected no error for healthy HPA, got: %v", err)
	}
}

func TestRunStatus_ExitCode_ScalingInactive(t *testing.T) {
	hpa := testutil.BuildHPA("default", "broken",
		testutil.WithReplicas(2, 0),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "broken", true)
	if err == nil {
		t.Fatal("expected ExitCodeError for ScalingActive=False, got nil")
	}
	exitErr, ok := err.(*ExitCodeError)
	if !ok {
		t.Fatalf("expected *ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitWarning {
		t.Fatalf("expected exit code %d (ExitWarning), got %d", ExitWarning, exitErr.Code)
	}
}

func TestRunStatus_ExitCode_NotFound(t *testing.T) {
	fakeClient := testutil.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent HPA, got nil")
	}
	if _, ok := err.(*ExitCodeError); ok {
		t.Fatalf("expected regular error for not-found, got *ExitCodeError")
	}
	if !errors.Is(err, ErrHPANotFound) {
		t.Errorf("expected ErrHPANotFound, got: %v", err)
	}
}

func TestRunStatus_ExitCode_ScalingLimited(t *testing.T) {
	hpa := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	if err == nil {
		t.Fatal("expected ExitCodeError for ScalingLimited, got nil")
	}
	exitErr, ok := err.(*ExitCodeError)
	if !ok {
		t.Fatalf("expected *ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitWarning {
		t.Fatalf("expected exit code %d (ExitWarning), got %d", ExitWarning, exitErr.Code)
	}
}

// isExitCodeWarning returns true if err is an *ExitCodeError with ExitWarning code.
func isExitCodeWarning(err error) bool {
	exitErr, ok := err.(*ExitCodeError)
	return ok && exitErr.Code == ExitWarning
}

// --------------------------------------------------------------------------
// --explain-pods integration tests
// --------------------------------------------------------------------------

func TestRunStatus_ExplainPods(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				ExplainPods: true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Error("expected output to contain HPA name")
	}
}

func TestRunStatus_ExplainPods_JSON(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				ExplainPods: true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	_ = report.Analysis.PodAnalysis
}

// --------------------------------------------------------------------------
// --simulate integration tests
// --------------------------------------------------------------------------

func TestRunStatus_Simulate(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 70),
		testutil.WithMinMax(1, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events:   EventOption{Enabled: false},
			Simulate: []string{"maxReplicas=20"},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if report.Analysis.Simulation == nil {
		t.Fatal("expected Simulation to be populated")
	}
	if report.Analysis.Simulation.Parameter != "maxReplicas" {
		t.Errorf("expected parameter=maxReplicas, got %q", report.Analysis.Simulation.Parameter)
	}
	if report.Analysis.Simulation.OriginalValue != "10" {
		t.Errorf("expected originalValue=10, got %q", report.Analysis.Simulation.OriginalValue)
	}
	if report.Analysis.Simulation.SimulatedValue != "20" {
		t.Errorf("expected simulatedValue=20, got %q", report.Analysis.Simulation.SimulatedValue)
	}
}

func TestRunStatus_SimulateText(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 70),
		testutil.WithMinMax(1, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events:   EventOption{Enabled: false},
			Simulate: []string{"maxReplicas=20"},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Simulation") {
		t.Error("expected output to contain 'Simulation' section")
	}
	if !strings.Contains(output, "maxReplicas") {
		t.Error("expected output to contain 'maxReplicas'")
	}
}

func TestParseSimulateOverrides(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "single override",
			input: []string{"maxReplicas=20"},
			want:  map[string]string{"maxReplicas": "20"},
		},
		{
			name:  "multiple overrides",
			input: []string{"maxReplicas=20", "minReplicas=3"},
			want:  map[string]string{"maxReplicas": "20", "minReplicas": "3"},
		},
		{
			name:    "no equals sign",
			input:   []string{"maxReplicas"},
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   []string{"=20"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSimulateOverrides(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d overrides, got %d", len(tt.want), len(got))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("override[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// --capacity-context integration tests
// --------------------------------------------------------------------------

func TestRunStatus_CapacityContext(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				CapacityContext: true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if report.Analysis.CapacityContext == nil {
		t.Error("expected CapacityContext to be populated")
	}
}
