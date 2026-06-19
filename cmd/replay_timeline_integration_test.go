package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds the replay, why-not-scale, container-advisor, ownership,
// profile-detect, and retrospective-timeline integration tests. They were
// split out of root_integration_test.go so each command area has its own
// focused integration test file.

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
		Common: commonOptions{
			Color: "never",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
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
		Common: commonOptions{
			Output: "markdown",
			Color:  "never",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
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
		Common: commonOptions{
			Color: "never",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
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

	candidate := testutil.BuildHPA("prod", "web", testutil.WithMinMax(2, 14))
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
	opts := &options{
		Common: commonOptions{
			Namespace: "prod",
			Color:     "never",
		},
	}
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
	opts := &options{
		Common: commonOptions{
			Namespace: "prod",
			Color:     "never",
		},
	}
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

	stablePath := writeTempCandidateHPA(t, testutil.BuildHPA("prod", "web", testutil.WithMinMax(2, 20)))
	defer func() { _ = os.Remove(stablePath) }()
	fastPath := writeTempCandidateHPA(t, testutil.BuildHPA("prod", "web", testutil.WithMinMax(2, 30)))
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
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithMinMax(2, 10),
		testutil.WithResourceMetric("cpu", 80, 120),
		testutil.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
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
	hpa := testutil.BuildHPA("default", "api",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 60, 90),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
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
	hpa := testutil.BuildHPA("default", "web", testutil.WithResourceMetric("cpu", 60, 70))
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
	fakeClient := testutil.NewFakeClient(hpa)
	_, err := fakeClient.AppsV1().Deployments("default").Create(context.Background(), deploy, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create deployment: %v", err)
	}

	var buf bytes.Buffer
	runOpts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
	}
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
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(6, 8),
		testutil.WithResourceMetric("cpu", 80, 70),
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
	fakeClient := testutil.NewFakeClient(hpa)
	if _, err := fakeClient.AppsV1().Deployments("default").Create(context.Background(), deploy, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create fake deployment: %v", err)
	}

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
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
	fakeClient := testutil.NewFakeClient()
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
	}

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
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))

	ev1 := testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale",
		"New size: 5; reason: cpu resource utilization above target", now.Add(-20*time.Minute))
	ev2 := testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale",
		"New size: 3", now.Add(-5*time.Minute))

	fakeClient := testutil.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1, ev2},
	)

	var buf bytes.Buffer
	err := runRetrospectiveTimeline(context.Background(), &buf, &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Color:          "never",
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
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))

	ev1 := testutil.BuildEventWithTimestamp("default", "web", "SuccessfulRescale",
		"New size: 5", now.Add(-10*time.Minute))

	fakeClient := testutil.NewFakeClientWithEvents(
		[]*autoscalingv2.HorizontalPodAutoscaler{hpa},
		[]*corev1.Event{ev1},
	)

	var buf bytes.Buffer
	err := runRetrospectiveTimeline(context.Background(), &buf, &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
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
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(3, 5))
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	err := runRetrospectiveTimeline(context.Background(), &buf, &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Color:          "never",
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
