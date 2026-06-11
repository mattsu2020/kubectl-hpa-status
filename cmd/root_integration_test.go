package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: true, limit: 5},
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			suggest: true,
			events:  eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			in:             io.Reader(strings.NewReader("")),
		},
		statusOptions: statusOptions{
			apply:  true,
			dryRun: false,
			yes:    true,
			events: eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			in:             io.Reader(strings.NewReader("")),
		},
		statusOptions: statusOptions{
			apply:  true,
			dryRun: true,
			yes:    true,
			events: eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "broken",
		kube.WithReplicas(2, 0),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	fakeClient := kube.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
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
		commonOptions: commonOptions{
			output:         "json",
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api", kube.WithReplicas(2, 2))
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api", kube.WithReplicas(2, 2))
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			output:         "json",
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			output:         "yaml",
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	ev1 := kube.BuildEvent("default", "web", "SuccessfulRescale", "New size: 5")
	ev2 := kube.BuildEvent("default", "web", "DesiredReplicasComputed", "calculated 5")
	fakeClient := kube.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1, ev2},
	)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: true, limit: 5},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			filter: "error",
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			problem: true,
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(2, 2),
		kube.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			healthScoreMax: 80,
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
	smallHPA := kube.BuildHPA("default", "small", kube.WithReplicas(1, 2))
	largeHPA := kube.BuildHPA("default", "large", kube.WithReplicas(5, 10))
	fakeClient := kube.NewFakeClient(largeHPA, smallHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			sortBy: "desired",
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithMinMax(2, 10),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			wide:           true,
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	webHPA := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	webHPA.Labels = map[string]string{"app": "web", "tier": "frontend"}
	apiHPA := kube.BuildHPA("default", "api", kube.WithReplicas(2, 2))
	apiHPA.Labels = map[string]string{"app": "api", "tier": "backend"}
	fakeClient := kube.NewFakeClient(webHPA, apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			selector:       "app=web",
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			in:             io.Reader(strings.NewReader("")),
		},
		statusOptions: statusOptions{
			apply:  true,
			dryRun: true,
			yes:    true,
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			problem: true,
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
	apiHPA := kube.BuildHPA("default", "api",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(apiHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			in:             io.Reader(strings.NewReader("n\n")),
		},
		statusOptions: statusOptions{
			apply:  true,
			dryRun: true,
			yes:    false,
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			problem: true,
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			in:             io.Reader(strings.NewReader("")),
		},
		statusOptions: statusOptions{
			apply:  true,
			dryRun: true,
			yes:    true,
			events: eventOption{enabled: false},
		},
		listOptions: listOptions{
			healthScoreMax: 80,
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
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 3))
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
		watchOptions: watchOptions{
			watchInterval: 100 * time.Millisecond,
			watchTimeout:  250 * time.Millisecond,
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
		watchOptions: watchOptions{
			watchInterval:  100 * time.Millisecond,
			untilCondition: "scaling-limited",
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
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
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	fakeClient := kube.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
	}
	err := runStatus(context.Background(), &buf, opts, "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent HPA, got nil")
	}
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
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events:      eventOption{enabled: false},
			explainPods: true,
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			output:         "json",
		},
		statusOptions: statusOptions{
			events:      eventOption{enabled: false},
			explainPods: true,
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 80, 70),
		kube.WithMinMax(1, 10),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			output:         "json",
		},
		statusOptions: statusOptions{
			events:   eventOption{enabled: false},
			simulate: []string{"maxReplicas=20"},
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 80, 70),
		kube.WithMinMax(1, 10),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
		statusOptions: statusOptions{
			events:   eventOption{enabled: false},
			simulate: []string{"maxReplicas=20"},
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
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			output:         "json",
		},
		statusOptions: statusOptions{
			events:          eventOption{enabled: false},
			capacityContext: true,
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

// --------------------------------------------------------------------------
// Replay integration tests
// --------------------------------------------------------------------------

func TestRunReplay_FromFile(t *testing.T) {
	trace := hpaanalysis.TimelineTrace{
		HPAName:   "web",
		Namespace: "default",
		Start:     time.Now(),
		Interval:  5 * time.Second,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{
				Timestamp:   time.Now(),
				Current:     3,
				Desired:     3,
				Health:      "OK",
				HealthScore: 100,
				TopMetric:   "cpu (ratio=0.90 within target)",
				Summary:     "steady",
			},
		},
	}

	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("failed to marshal trace: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "hpa-trace-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("failed to write trace: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			color: "never",
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
	}

	err = runReplay(&buf, opts, tmpFile.Name())
	if err != nil {
		t.Fatalf("runReplay returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Error("expected output to contain HPA name")
	}
	if !strings.Contains(output, "TIME") {
		t.Error("expected output to contain table header")
	}
}

func TestRunReplay_Markdown(t *testing.T) {
	trace := hpaanalysis.TimelineTrace{
		HPAName:   "web",
		Namespace: "default",
		Start:     time.Now(),
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{
				Timestamp:   time.Now(),
				Current:     3,
				Desired:     5,
				Health:      "LIMITED",
				HealthScore: 75,
			},
		},
	}

	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("failed to marshal trace: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "hpa-trace-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("failed to write trace: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			output: "markdown",
			color:  "never",
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
	}

	err = runReplay(&buf, opts, tmpFile.Name())
	if err != nil {
		t.Fatalf("runReplay returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "# HPA Timeline") {
		t.Error("expected markdown header")
	}
}

func TestRunReplay_FileNotFound(t *testing.T) {
	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			color: "never",
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
	}

	err := runReplay(&buf, opts, "/nonexistent/path.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRunReplayLab_FromRecordWithCandidate(t *testing.T) {
	now := time.Now()
	trace := hpaanalysis.TimelineTrace{
		HPAName:   "web",
		Namespace: "prod",
		Start:     now,
		Interval:  5 * time.Second,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: now, Desired: 3, Health: "OK"},
			{Timestamp: now.Add(5 * time.Second), Desired: 8, Health: "OK"},
			{Timestamp: now.Add(10 * time.Second), Desired: 12, Health: "LIMITED"},
			{Timestamp: now.Add(15 * time.Second), Desired: 6, Health: "OK"},
		},
	}
	recordFile, err := os.CreateTemp("", "hpa-record-*.jsonl")
	if err != nil {
		t.Fatalf("failed to create record file: %v", err)
	}
	defer func() { _ = os.Remove(recordFile.Name()) }()
	if err := writeRecordLine(recordFile, trace); err != nil {
		t.Fatalf("failed to write record line: %v", err)
	}
	if err := recordFile.Close(); err != nil {
		t.Fatalf("failed to close record file: %v", err)
	}

	candidate := kube.BuildHPA("prod", "web", kube.WithMinMax(2, 14))
	candidateData, err := json.Marshal(candidate)
	if err != nil {
		t.Fatalf("failed to marshal candidate: %v", err)
	}
	candidateFile, err := os.CreateTemp("", "candidate-hpa-*.yaml")
	if err != nil {
		t.Fatalf("failed to create candidate file: %v", err)
	}
	defer func() { _ = os.Remove(candidateFile.Name()) }()
	if _, err := candidateFile.Write(candidateData); err != nil {
		t.Fatalf("failed to write candidate file: %v", err)
	}
	if err := candidateFile.Close(); err != nil {
		t.Fatalf("failed to close candidate file: %v", err)
	}

	var buf bytes.Buffer
	opts := &options{commonOptions: commonOptions{namespace: "prod", color: "never"}}
	if err := runReplayLab(&buf, opts, "web", recordFile.Name(), candidateFile.Name(), nil); err != nil {
		t.Fatalf("runReplayLab returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Replay Summary: web / prod") ||
		!strings.Contains(output, "Proposed") ||
		!strings.Contains(output, "capped duration") ||
		!strings.Contains(output, "additional worst-case pods: +2") {
		t.Fatalf("expected replay lab comparison, got:\n%s", output)
	}
}

func TestRunReplayLab_FromRecordWithSetOverrides(t *testing.T) {
	now := time.Now()
	trace := hpaanalysis.TimelineTrace{
		HPAName:   "web",
		Namespace: "prod",
		Start:     now,
		Interval:  5 * time.Second,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: now, Desired: 4, Health: "OK"},
			{Timestamp: now.Add(5 * time.Second), Desired: 12, Health: "LIMITED"},
			{Timestamp: now.Add(10 * time.Second), Desired: 5, Health: "OK"},
			{Timestamp: now.Add(15 * time.Second), Desired: 11, Health: "OK"},
		},
	}
	recordFile, err := os.CreateTemp("", "hpa-record-*.jsonl")
	if err != nil {
		t.Fatalf("failed to create record file: %v", err)
	}
	defer func() { _ = os.Remove(recordFile.Name()) }()
	if err := writeRecordLine(recordFile, trace); err != nil {
		t.Fatalf("failed to write record line: %v", err)
	}
	if err := recordFile.Close(); err != nil {
		t.Fatalf("failed to close record file: %v", err)
	}

	var buf bytes.Buffer
	opts := &options{commonOptions: commonOptions{namespace: "prod", color: "never"}}
	overrides := map[string]string{
		"maxReplicas":                          "20",
		"scaleDown.stabilizationWindowSeconds": "600",
	}
	if err := runReplayLab(&buf, opts, "web", recordFile.Name(), "", overrides); err != nil {
		t.Fatalf("runReplayLab returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Proposed") ||
		!strings.Contains(output, "maxReplicas: 20") ||
		!strings.Contains(output, "scaleDown.stabilizationWindowSeconds: 600") ||
		!strings.Contains(output, "estimated extra pod-hours") ||
		!strings.Contains(output, "additional worst-case pods: +8") {
		t.Fatalf("expected replay lab --set comparison, got:\n%s", output)
	}
}

func TestReplayCommandFileWithHPAShortcutFlags(t *testing.T) {
	now := time.Now()
	trace := hpaanalysis.TimelineTrace{
		HPAName:   "web",
		Namespace: "prod",
		Start:     now,
		Interval:  5 * time.Second,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: now, Desired: 4, Health: "OK"},
			{Timestamp: now.Add(5 * time.Second), Desired: 12, Health: "LIMITED"},
			{Timestamp: now.Add(10 * time.Second), Desired: 8, Health: "OK"},
		},
	}
	recordFile, err := os.CreateTemp("", "hpa-record-*.jsonl")
	if err != nil {
		t.Fatalf("failed to create record file: %v", err)
	}
	defer func() { _ = os.Remove(recordFile.Name()) }()
	if err := writeRecordLine(recordFile, trace); err != nil {
		t.Fatalf("failed to write record line: %v", err)
	}
	if err := recordFile.Close(); err != nil {
		t.Fatalf("failed to close record file: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"replay", recordFile.Name(),
		"--hpa", "web",
		"-n", "prod",
		"--set-max-replicas", "30",
		"--set-scale-down-stabilization", "600s",
		"--set-cpu-target", "70",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("replay shortcut command returned error: %v\n%s", err, buf.String())
	}
	output := buf.String()
	if !strings.Contains(output, "Scenario comparison: prod/web") ||
		!strings.Contains(output, "maxReplicas: 30") ||
		!strings.Contains(output, "cpu.targetAverageUtilization: 70") ||
		!strings.Contains(output, "estimated pod-hours") {
		t.Fatalf("expected replay shortcut output, got:\n%s", output)
	}
}

func TestReplayCommandPolicyLabMultipleCandidatesInfersHPA(t *testing.T) {
	now := time.Now()
	trace := hpaanalysis.TimelineTrace{
		HPAName:   "web",
		Namespace: "prod",
		Start:     now,
		Interval:  60 * time.Second,
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Timestamp: now, Desired: 4, Health: "OK"},
			{Timestamp: now.Add(60 * time.Second), Desired: 12, Health: "LIMITED"},
			{Timestamp: now.Add(120 * time.Second), Desired: 6, Health: "OK"},
			{Timestamp: now.Add(180 * time.Second), Desired: 11, Health: "OK"},
		},
	}
	recordFile, err := os.CreateTemp("", "hpa-record-*.jsonl")
	if err != nil {
		t.Fatalf("failed to create record file: %v", err)
	}
	defer func() { _ = os.Remove(recordFile.Name()) }()
	if err := writeRecordLine(recordFile, trace); err != nil {
		t.Fatalf("failed to write record line: %v", err)
	}
	if err := recordFile.Close(); err != nil {
		t.Fatalf("failed to close record file: %v", err)
	}

	stablePath := writeTempCandidateHPA(t, kube.BuildHPA("prod", "web", kube.WithMinMax(2, 20)))
	defer func() { _ = os.Remove(stablePath) }()
	fastPath := writeTempCandidateHPA(t, kube.BuildHPA("prod", "web", kube.WithMinMax(2, 30)))
	defer func() { _ = os.Remove(fastPath) }()

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"replay", recordFile.Name(),
		"-n", "prod",
		"--candidate", stablePath,
		"--candidate", fastPath,
		"--score", "slo,cost,churn",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("replay policy lab returned error: %v\n%s", err, buf.String())
	}
	output := buf.String()
	for _, want := range []string{
		"Replay Summary: web / prod",
		"Score focus: slo,cost,churn",
		"Candidate",
		"Replica-minutes",
		"Recommended:",
		stablePath,
		fastPath,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func writeTempCandidateHPA(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	t.Helper()
	data, err := json.Marshal(hpa)
	if err != nil {
		t.Fatalf("failed to marshal candidate: %v", err)
	}
	file, err := os.CreateTemp("", "candidate-hpa-*.yaml")
	if err != nil {
		t.Fatalf("failed to create candidate file: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatalf("failed to write candidate file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("failed to close candidate file: %v", err)
	}
	return file.Name()
}

func TestRunWhyNotScaleShowsObservedBlockersAndUnknowns(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(10, 10),
		kube.WithMinMax(2, 10),
		kube.WithResourceMetric("cpu", 80, 120),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
	}
	if err := runWhyNotScale(context.Background(), &buf, opts, []string{"web"}); err != nil {
		t.Fatalf("runWhyNotScale returned error: %v", err)
	}
	output := buf.String()
	for _, want := range []string{
		"Why not scale: default/web",
		"HPA is at maxReplicas",
		"Resource metric cpu ratio=1.50",
		"maxReplicas may be capping scale-up",
		"controller-internal per-metric replica recommendations are not exposed",
		"kubectl describe hpa web -n default",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestRunWhyNotScaleJSON(t *testing.T) {
	hpa := kube.BuildHPA("default", "api",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 60, 90),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			output:         "json",
		},
	}
	if err := runWhyNotScale(context.Background(), &buf, opts, []string{"api"}); err != nil {
		t.Fatalf("runWhyNotScale returned error: %v", err)
	}
	var report whyNotScaleReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}
	if report.Name != "api" || report.Namespace != "default" {
		t.Fatalf("unexpected report identity: %#v", report)
	}
	if len(report.Observed) == 0 || len(report.Unknown) == 0 || len(report.NextChecks) == 0 {
		t.Fatalf("expected observed/unknown/next checks in report: %#v", report)
	}
}

func TestAdvisorContainerResourceCommand(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithResourceMetric("cpu", 60, 70))
	replicas := int32(2)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{
						Name: "app",
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						}},
					},
					{
						Name: "istio-proxy",
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						}},
					},
				}},
			},
		},
	}
	fakeClient := kube.NewFakeClient(hpa)
	_, err := fakeClient.AppsV1().Deployments("default").Create(context.Background(), deploy, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create deployment: %v", err)
	}

	var buf bytes.Buffer
	runOpts := &options{commonOptions: commonOptions{clientOverride: fakeClient}}
	if err := runContainerAdvisor(context.Background(), &buf, runOpts, []string{"web"}); err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runContainerAdvisor returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Container Resource Advisor") ||
		!strings.Contains(output, "ContainerResource") ||
		!strings.Contains(output, "container: app") {
		t.Fatalf("expected container-resource advisor output, got:\n%s", output)
	}
}

func TestOwnershipCommandDetectsReplicaFieldOwner(t *testing.T) {
	replicas := int32(3)
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(6, 8),
		kube.WithResourceMetric("cpu", 80, 70),
	)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "default",
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:    "argocd-controller",
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: "apps/v1",
					FieldsType: "FieldsV1",
					FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)},
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			},
		},
	}
	fakeClient := kube.NewFakeClient(hpa)
	if _, err := fakeClient.AppsV1().Deployments("default").Create(context.Background(), deploy, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create fake deployment: %v", err)
	}

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
		},
	}

	if err := runOwnership(context.Background(), &buf, opts, []string{"web"}); err != nil {
		t.Fatalf("runOwnership returned error: %v", err)
	}
	output := buf.String()
	for _, want := range []string{
		"Scale ownership: default/web",
		"argocd-controller",
		"spec.replicas=3 differs from HPA desiredReplicas=8",
		"remove spec.replicas from GitOps manifests",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestRunProfileDetectShowsAssumptions(t *testing.T) {
	fakeClient := kube.NewFakeClient()
	opts := &options{commonOptions: commonOptions{clientOverride: fakeClient}}

	var buf bytes.Buffer
	if err := runProfileDetect(context.Background(), &buf, opts); err != nil {
		t.Fatalf("runProfileDetect returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA Controller Profile") ||
		!strings.Contains(output, "Assumed / Effective") ||
		!strings.Contains(output, "--controller-profile-file") {
		t.Fatalf("expected profile detector output, got:\n%s", output)
	}
}

// --------------------------------------------------------------------------
// Timeline --since (retrospective) integration tests
// --------------------------------------------------------------------------

func TestRunTimeline_Retrospective(t *testing.T) {
	now := time.Now()
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))

	ev1 := kube.BuildEventWithTimestamp("default", "web", "SuccessfulRescale",
		"New size: 5; reason: cpu resource utilization above target", now.Add(-20*time.Minute))
	ev2 := kube.BuildEventWithTimestamp("default", "web", "SuccessfulRescale",
		"New size: 3", now.Add(-5*time.Minute))

	fakeClient := kube.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1, ev2},
	)

	var buf bytes.Buffer
	err := runRetrospectiveTimeline(context.Background(), &buf, &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			color:          "never",
		},
	}, "web", 30*time.Minute, false)
	if err != nil {
		t.Fatalf("runRetrospectiveTimeline returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "HPA Scaling Timeline: web (default)") {
		t.Errorf("expected timeline header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "desired 3 -> 5") {
		t.Errorf("expected scale-up entry in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Best-effort") {
		t.Errorf("expected disclaimer in output, got:\n%s", output)
	}
}

func TestRunTimeline_Retrospective_JSON(t *testing.T) {
	now := time.Now()
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))

	ev1 := kube.BuildEventWithTimestamp("default", "web", "SuccessfulRescale",
		"New size: 5", now.Add(-10*time.Minute))

	fakeClient := kube.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1},
	)

	var buf bytes.Buffer
	err := runRetrospectiveTimeline(context.Background(), &buf, &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			output:         "json",
		},
	}, "web", 30*time.Minute, false)
	if err != nil {
		t.Fatalf("runRetrospectiveTimeline JSON returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"hpaName": "web"`) {
		t.Errorf("expected hpaName in JSON output, got:\n%s", output)
	}
	if !strings.Contains(output, `"entries"`) {
		t.Errorf("expected entries in JSON output, got:\n%s", output)
	}
}

func TestRunTimeline_Retrospective_NoEvents(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithReplicas(3, 5))
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	err := runRetrospectiveTimeline(context.Background(), &buf, &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			color:          "never",
		},
	}, "web", 30*time.Minute, false)
	if err != nil {
		t.Fatalf("runRetrospectiveTimeline returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No scaling events found") {
		t.Errorf("expected 'No scaling events found' when no events exist, got:\n%s", output)
	}
}
