package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// --------------------------------------------------------------------------
// Status command integration tests
// --------------------------------------------------------------------------

func TestRunStatus_OK(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: true, limit: 5},
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	// ScalingLimited produces LIMITED health -> ExitCodeError with code 2.
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		suggest:        true,
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	// LIMITED health returns ExitCodeError; that is expected.
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "kubectl patch hpa api") {
		t.Fatalf("expected patch command in suggest output, got:\n%s", output)
	}
}

func TestRunStatusApplyPatchesHPA(t *testing.T) {
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		apply:          true,
		dryRun:         false,
		yes:            true,
		in:             io.Reader(strings.NewReader("")),
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	// LIMITED health returns ExitCodeError; that is expected.
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		apply:          true,
		dryRun:         true,
		yes:            true,
		in:             io.Reader(strings.NewReader("")),
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "api", true)
	// LIMITED health returns ExitCodeError; that is expected.
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
	hpa := kube.BuildHPA("default", "broken",
		kube.WithReplicas(2, 0),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "broken", true)
	// ERROR health returns ExitCodeError with code 2.
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
	fakeClient := kube.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent HPA, got nil")
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected not-found error message, got: %v", err)
	}
}

func TestRunStatus_JSONOutput(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		output:         "json",
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api", kube.WithReplicas(2, 2))
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api", kube.WithReplicas(2, 2))
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		output:         "json",
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		output:         "yaml",
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	ev1 := kube.BuildEvent("default", "web", "SuccessfulRescale", "New size: 5")
	ev2 := kube.BuildEvent("default", "web", "DesiredReplicasComputed", "calculated 5")
	fakeClient := kube.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1, ev2},
	)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: true, limit: 5},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		filter:         "error",
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		problem:        true,
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		healthScoreMax: 80,
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	smallHPA := kube.BuildHPA("default", "small", kube.WithReplicas(1, 2))
	largeHPA := kube.BuildHPA("default", "large", kube.WithReplicas(5, 10))
	fakeClient := kube.NewFakeClient(largeHPA, smallHPA)

	var buf bytes.Buffer
	opts := &options{
		sortBy:         "desired",
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithMinMax(2, 10),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		wide:           true,
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	webHPA.Labels = map[string]string{"app": "web", "tier": "frontend"}
	apiHPA := kube.BuildHPA("default", "api", kube.WithReplicas(2, 2))
	apiHPA.Labels = map[string]string{"app": "api", "tier": "backend"}
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		selector:       "app=web",
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	webHPA := kube.BuildHPA("default", "web",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(apiHPA, webHPA)

	var buf bytes.Buffer
	opts := &options{
		apply:          true,
		dryRun:         true,
		yes:            true,
		in:             io.Reader(strings.NewReader("")),
		problem:        true,
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(apiHPA)

	var buf bytes.Buffer
	opts := &options{
		apply:          true,
		dryRun:         true,
		yes:            false,
		in:             io.Reader(strings.NewReader("n\n")),
		problem:        true,
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	// Healthy HPA with no applicable suggestions
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		apply:          true,
		dryRun:         true,
		yes:            true,
		in:             io.Reader(strings.NewReader("")),
		healthScoreMax: 80, // will not match since health is 100
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	// The HPA is healthy (score 100) but we filter for score <= 80, so nothing matches.
	// The apply flow runs only on filtered items, so no patches.
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Watch command integration tests
// --------------------------------------------------------------------------

func TestRunWatch_TimeoutExpires(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 3))
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
		watchInterval:  100 * time.Millisecond,
		watchTimeout:   250 * time.Millisecond,
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
		watchInterval:  100 * time.Millisecond,
		untilCondition: "scaling-limited",
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "web", true)
	if err != nil {
		t.Fatalf("expected no error for healthy HPA, got: %v", err)
	}
}

func TestRunStatus_ExitCode_ScalingInactive(t *testing.T) {
	hpa := kube.BuildHPA("default", "broken",
		kube.WithReplicas(2, 0),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
	fakeClient := kube.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
	}
	err := runStatus(context.Background(), &buf, opts, "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent HPA, got nil")
	}
	// Not-found should be a regular error, not an ExitCodeError.
	if _, ok := err.(*ExitCodeError); ok {
		t.Fatalf("expected regular error for not-found, got *ExitCodeError")
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected not-found error message, got: %v", err)
	}
}

func TestRunStatus_ExitCode_ScalingLimited(t *testing.T) {
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		clientOverride: fakeClient,
		events:         eventOption{enabled: false},
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
