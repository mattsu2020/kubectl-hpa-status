package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

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

	// Multi-HPA output uses the StatusBatch envelope: {"apiVersion":..., "items":[...]}.
	var batch hpaanalysis.StatusBatch
	if err := json.Unmarshal(buf.Bytes(), &batch); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if batch.APIVersion != hpaanalysis.SchemaVersion {
		t.Fatalf("unexpected apiVersion %q", batch.APIVersion)
	}
	if len(batch.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(batch.Items), batch.Items)
	}
	for i, want := range []struct{ name, status string }{{"web", "ok"}, {"api", "ok"}} {
		item := batch.Items[i]
		if item.Name != want.name || string(item.Status) != want.status || item.Report == nil {
			t.Fatalf("item %d: want name=%s status=%s report!=nil, got %#v", i, want.name, want.status, item)
		}
	}
}

// --------------------------------------------------------------------------
// Multi-HPA partial-result tests
//
// These cover the partial-result contract: a per-item fetch/build failure
// does not abort the whole batch. The failed item is surfaced in the output
// envelope / text, and the exit code reflects the most severe per-item
// outcome (error > warning > ok).
// --------------------------------------------------------------------------

func TestRunStatusMany_PartialFailure_TextOutput(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	fakeClient := testutil.NewFakeClient(webHPA) // "missing" is absent

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}
	err := runStatusMany(context.Background(), &buf, opts, []string{"web", "missing"}, false)

	// Exit code is ExitError (1) because one item failed to build.
	exitErr, ok := err.(*ExitCodeError)
	if !ok || exitErr.Code != ExitError {
		t.Fatalf("expected *ExitCodeError with ExitError, got %T: %v", err, err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected successful item 'web' in text output, got:\n%s", output)
	}
	if !strings.Contains(output, "Error:") || !strings.Contains(output, "missing") {
		t.Errorf("expected an Error: line naming 'missing' in text output, got:\n%s", output)
	}
}

func TestRunStatusMany_PartialFailure_JSONOutput(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	fakeClient := testutil.NewFakeClient(webHPA)

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
	err := runStatusMany(context.Background(), &buf, opts, []string{"web", "missing"}, false)
	exitErr, ok := err.(*ExitCodeError)
	if !ok || exitErr.Code != ExitError {
		t.Fatalf("expected *ExitCodeError with ExitError, got %T: %v", err, err)
	}

	var batch hpaanalysis.StatusBatch
	if err := json.Unmarshal(buf.Bytes(), &batch); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if len(batch.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(batch.Items))
	}
	if batch.Items[0].Name != "web" || string(batch.Items[0].Status) != "ok" || batch.Items[0].Report == nil {
		t.Errorf("item 0: expected web/ok/report, got %#v", batch.Items[0])
	}
	if batch.Items[1].Name != "missing" || string(batch.Items[1].Status) != "error" || batch.Items[1].Report != nil || batch.Items[1].Error == "" {
		t.Errorf("item 1: expected missing/error/no-report, got %#v", batch.Items[1])
	}
}

func TestRunStatusMany_PartialFailure_YAMLOutput(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	fakeClient := testutil.NewFakeClient(webHPA)

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
	err := runStatusMany(context.Background(), &buf, opts, []string{"web", "missing"}, false)
	exitErr, ok := err.(*ExitCodeError)
	if !ok || exitErr.Code != ExitError {
		t.Fatalf("expected *ExitCodeError with ExitError, got %T: %v", err, err)
	}
	output := buf.String()
	if !strings.Contains(output, "name: web") || !strings.Contains(output, "status: ok") {
		t.Errorf("expected web/ok item in YAML, got:\n%s", output)
	}
	if !strings.Contains(output, "name: missing") || !strings.Contains(output, "status: error") {
		t.Errorf("expected missing/error item in YAML, got:\n%s", output)
	}
}

func TestRunStatusMany_PartialFailure_AllFail(t *testing.T) {
	fakeClient := testutil.NewFakeClient() // both names absent

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
	err := runStatusMany(context.Background(), &buf, opts, []string{"a", "b"}, false)
	exitErr, ok := err.(*ExitCodeError)
	if !ok || exitErr.Code != ExitError {
		t.Fatalf("expected *ExitCodeError with ExitError, got %T: %v", err, err)
	}

	var batch hpaanalysis.StatusBatch
	if err := json.Unmarshal(buf.Bytes(), &batch); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput:\n%s", err, buf.String())
	}
	if len(batch.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(batch.Items))
	}
	for i, item := range batch.Items {
		if string(item.Status) != "error" || item.Report != nil || item.Error == "" {
			t.Errorf("item %d: expected error/no-report, got %#v", i, item)
		}
	}
}

func TestRunStatusMany_PartialFailure_WarningAggregation(t *testing.T) {
	// web: healthy, scaling up.
	webHPA := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	// api: at maxReplicas with ScalingLimited -> health LIMITED.
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
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
	err := runStatusMany(context.Background(), &buf, opts, []string{"web", "api"}, false)
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning (most severe is LIMITED), got: %v", err)
	}
}

func TestAggregateBatchExitCode_AllOK(t *testing.T) {
	results := []reportResult{
		{name: "a", hasReport: true, report: hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Health: string(hpaanalysis.HealthOK)}}},
		{name: "b", hasReport: true, report: hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Health: string(hpaanalysis.HealthStabilized)}}},
	}
	if err := aggregateBatchExitCode(results, false); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestAggregateBatchExitCode_HasWarning(t *testing.T) {
	results := []reportResult{
		{name: "a", hasReport: true, report: hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Health: string(hpaanalysis.HealthOK)}}},
		{name: "b", hasReport: true, report: hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Health: string(hpaanalysis.HealthLimited)}}},
	}
	err := aggregateBatchExitCode(results, false)
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitWarning, got %v", err)
	}
}

func TestAggregateBatchExitCode_HasError(t *testing.T) {
	results := []reportResult{
		{name: "a", hasReport: true, report: hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Health: string(hpaanalysis.HealthLimited)}}},
		{name: "b", hasReport: false, err: errors.New("not found")},
	}
	err := aggregateBatchExitCode(results, false)
	exitErr, ok := err.(*ExitCodeError)
	if !ok || exitErr.Code != ExitError {
		t.Fatalf("expected ExitError (error dominates warning), got %T: %v", err, err)
	}
}

func TestAggregateBatchExitCode_WatchModeSuppressesWarning(t *testing.T) {
	results := []reportResult{
		{name: "a", hasReport: true, report: hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{Health: string(hpaanalysis.HealthLimited)}}},
	}
	if err := aggregateBatchExitCode(results, true); err != nil {
		t.Fatalf("watch mode should suppress warning, got %v", err)
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
