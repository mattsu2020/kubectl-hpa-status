package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"

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
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "maxReplicas") {
		t.Errorf("expected maxReplicas mention in output, got:\n%s", output)
	}
	if !strings.Contains(output, "ScalingLimited") {
		t.Errorf("expected ScalingLimited condition in output, got:\n%s", output)
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
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
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
