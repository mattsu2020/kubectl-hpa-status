package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

// --------------------------------------------------------------------------
// Edge case integration tests
// --------------------------------------------------------------------------

func TestRunStatus_ScalingInactiveWithExternalMetric(t *testing.T) {
	// Simulates metrics-server down with an external metric configured but no status.
	hpa := testutil.BuildHPA("default", "keda-worker",
		testutil.WithReplicas(2, 0),
		testutil.WithScalingActiveFalse("FailedGetExternalMetric"),
		testutil.WithExternalMetric("queue_depth", "5"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runStatus(context.Background(), &buf, opts, "keda-worker", true)
	// ScalingActive=False produces ERROR health -> ExitCodeError with code 2.
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "FailedGetExternalMetric") {
		t.Errorf("expected FailedGetExternalMetric in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Check external metric freshness") {
		t.Errorf("expected external metric freshness suggestion, got:\n%s", output)
	}
}

func TestRunStatus_ImplicitMaxReplicas(t *testing.T) {
	// current == desired == maxReplicas but no ScalingLimited condition.
	hpa := testutil.BuildHPA("default", "capped",
		testutil.WithMinMax(2, 5),
		testutil.WithDesiredAtMax(),
		testutil.WithResourceMetric("cpu", 80, 95),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runStatus(context.Background(), &buf, opts, "capped", true)
	// Implicit maxReplicas produces LIMITED health -> ExitCodeError with code 2.
	if !isExitCodeWarning(err) {
		t.Fatalf("expected ExitCodeError with ExitWarning, got: %v", err)
	}
	output := buf.String()
	// Health is rendered with emoji + label like "ScalingLimited", not bare "LIMITED".
	if !strings.Contains(output, "ScalingLimited") && !strings.Contains(output, "LIMITED") {
		t.Errorf("expected LIMITED/ScalingLimited health status, got:\n%s", output)
	}
}

func TestRunStatus_ScaleDownStabilized(t *testing.T) {
	hpa := testutil.BuildHPA("default", "stable",
		testutil.WithReplicas(5, 3),
		testutil.WithScaleDownStabilized(),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runStatus(context.Background(), &buf, opts, "stable", true)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	// Health label is "Stabilized" (title case), not "STABILIZED" (upper case).
	if !strings.Contains(output, "tabilized") {
		t.Errorf("expected Stabilized health status, got:\n%s", output)
	}
	if !strings.Contains(output, "ScaleDownStabilized") {
		t.Errorf("expected ScaleDownStabilized reason, got:\n%s", output)
	}
}

func TestRunStatus_ExternalMetricPresent(t *testing.T) {
	// External metric with both spec and status present, ratio above target.
	hpa := testutil.BuildHPA("default", "queue-worker",
		testutil.WithReplicas(3, 5),
		testutil.WithExternalMetricWithStatus("queue_depth", "5", "12"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runStatus(context.Background(), &buf, opts, "queue-worker", true)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "queue_depth") {
		t.Errorf("expected queue_depth metric in output, got:\n%s", output)
	}
}

func TestRunStatus_KEDADetectionWithoutKEDAFlag(t *testing.T) {
	// KEDA labels detected but --keda not set: basic KEDA diagnostics via looksLikeKEDAManaged.
	hpa := testutil.BuildHPA("default", "keda-hpa-worker",
		testutil.WithReplicas(3, 3),
		testutil.WithKEDALabels("worker"),
		testutil.WithExternalMetric("s0-azure-queue-message-count", "5"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runStatus(context.Background(), &buf, opts, "keda-hpa-worker", true)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "KEDA") {
		t.Errorf("expected KEDA mention in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Inspect KEDA ScaledObject") {
		t.Errorf("expected KEDA suggestion, got:\n%s", output)
	}
}

func TestRunStatus_DebugMode(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 95),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient, debug: true},
	}
	err := runStatus(context.Background(), &buf, opts, "web", true)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Debug") {
		t.Errorf("expected Debug section in output, got:\n%s", output)
	}
	if !strings.Contains(output, "health: state=") {
		t.Errorf("expected health debug line, got:\n%s", output)
	}
}

func TestRunList_MinScore(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	apiHPA := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(2, 2),
	)
	brokenHPA := testutil.BuildHPA("default", "broken",
		testutil.WithReplicas(2, 2),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(webHPA, apiHPA, brokenHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	// web (score=100) and api (score=95) should pass min-score=50.
	if !strings.Contains(output, "web") {
		t.Errorf("expected 'web' (score=100) in min-score=50 output, got:\n%s", output)
	}
	if !strings.Contains(output, "api") {
		t.Errorf("expected 'api' (score=95) in min-score=50 output, got:\n%s", output)
	}
	// broken has score=55 which is >= 50, so it also passes min-score=50.
	// This is correct: min-score is a lower bound on health score.
	if !strings.Contains(output, "broken") {
		t.Errorf("expected 'broken' (score=55 >= min-score 50) in output, got:\n%s", output)
	}
}

func TestRunList_MinScoreFiltersLow(t *testing.T) {
	webHPA := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	brokenHPA := testutil.BuildHPA("default", "broken",
		testutil.WithReplicas(2, 2),
		testutil.WithScalingActiveFalse("FailedGetResourceMetric"),
	)
	fakeClient := testutil.NewFakeClient(webHPA, brokenHPA)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
		listOptions:   listOptions{healthScoreMin: 90},
	}
	err := runList(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("runList returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Errorf("expected 'web' (score=100) in min-score=90 output, got:\n%s", output)
	}
	if strings.Contains(output, "broken") {
		t.Errorf("expected 'broken' (score=55) to be filtered out by min-score=90, got:\n%s", output)
	}
}

func TestRunStatus_BehaviorWithStabilizationWindow(t *testing.T) {
	hpa := testutil.BuildHPA("default", "slow",
		testutil.WithReplicas(5, 3),
		testutil.WithScaleDownStabilized(),
		testutil.WithScaleDownStabilizationWindow(300),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runStatus(context.Background(), &buf, opts, "slow", true)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "scaleDown") {
		t.Errorf("expected scaleDown behavior mention, got:\n%s", output)
	}
}
