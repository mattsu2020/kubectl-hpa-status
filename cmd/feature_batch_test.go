package cmd

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestRunBehavior_TextOutput(t *testing.T) {
	podsPolicy := autoscalingv2.PodsScalingPolicy
	maxPolicy := autoscalingv2.MaxChangePolicySelect
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(10, 40),
	)
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &autoscalingv2.HPAScalingRules{
			SelectPolicy: &maxPolicy,
			Policies: []autoscalingv2.HPAScalingPolicy{{
				Type:          podsPolicy,
				Value:         10,
				PeriodSeconds: 15,
			}},
		},
	}
	fakeClient := kube.NewFakeClient(hpa)
	opts := &options{commonOptions: commonOptions{clientOverride: fakeClient}}

	var buf bytes.Buffer
	if err := runBehavior(context.Background(), &buf, opts, "web"); err != nil {
		t.Fatalf("runBehavior returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "ScaleUp behavior") || !strings.Contains(output, "t+15s") {
		t.Fatalf("expected behavior policy and estimated path, got:\n%s", output)
	}
}

func TestRunEstimate_TextOutput(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithMinMax(2, 10))
	fakeClient := kube.NewFakeClient(hpa)
	opts := &options{commonOptions: commonOptions{clientOverride: fakeClient}}

	var buf bytes.Buffer
	if err := runEstimate(context.Background(), &buf, opts, "web", 30, 0.12, 0.01); err != nil {
		t.Fatalf("runEstimate returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Additional worst-case pods: 20") || !strings.Contains(output, "$2.40/hour") || !strings.Contains(output, "0.2000 kgCO2e/hour") {
		t.Fatalf("expected cost estimate, got:\n%s", output)
	}
}

func TestWriteGitHubLintAnnotations(t *testing.T) {
	results := []lintFileResult{{
		File: "k8s/hpa.yaml",
		Result: &hpaanalysis.LintResult{Findings: []hpaanalysis.LintFinding{{
			Severity:       hpaanalysis.LintWarning,
			Rule:           "max-replicas",
			Message:        "maxReplicas may be too low",
			Recommendation: "raise maxReplicas after preflight",
		}}},
	}}

	var buf bytes.Buffer
	if err := writeGitHubLintAnnotations(&buf, results); err != nil {
		t.Fatalf("writeGitHubLintAnnotations returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "::warning file=k8s/hpa.yaml::maxReplicas may be too low") {
		t.Fatalf("expected GitHub annotation, got:\n%s", output)
	}
}

func TestLoadRecordedTrace_JSONL(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "hpa-history-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	first := hpaanalysis.TimelineTrace{
		Namespace: "default",
		HPAName:   "web",
		Start:     time.Now(),
		End:       time.Now(),
		Snapshots: []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now(), Current: 2, Desired: 2, Health: "OK"}},
	}
	second := first
	second.Snapshots = []hpaanalysis.TimelineSnapshot{{Timestamp: time.Now().Add(time.Second), Current: 2, Desired: 5, Health: "LIMITED"}}
	if err := writeRecordLine(tmp, first); err != nil {
		t.Fatal(err)
	}
	if err := writeRecordLine(tmp, second); err != nil {
		t.Fatal(err)
	}

	trace, err := loadRecordedTrace(tmp.Name(), "default", "web")
	if err != nil {
		t.Fatalf("loadRecordedTrace returned error: %v", err)
	}
	if len(trace.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(trace.Snapshots))
	}
}

func TestWriteClusterSummaryMarkdown(t *testing.T) {
	report := hpaanalysis.ListReport{Items: []hpaanalysis.ListItem{
		{Namespace: "prod", Name: "web", Health: "OK", HealthScore: 100},
		{Namespace: "prod", Name: "api", Health: "ERROR", HealthScore: 45, Issue: "FailedGetExternalMetric"},
	}}
	var buf bytes.Buffer
	if err := writeClusterSummaryMarkdown(&buf, report); err != nil {
		t.Fatalf("writeClusterSummaryMarkdown returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "# HPA Cluster Health Report") || !strings.Contains(output, "| prod | api | 45 | FailedGetExternalMetric |") {
		t.Fatalf("expected cluster summary markdown, got:\n%s", output)
	}
}

func TestAlertsGeneratePrometheus(t *testing.T) {
	cmd := newAlertsCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"generate", "--format", "prometheus"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("alerts generate returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "HPAScalingLimited") {
		t.Fatalf("expected prometheus alert rule, got:\n%s", buf.String())
	}
}

func TestRunAnalyzeRecordDetectsFlapping(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "hpa-history-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmp.Close() }()

	trace := hpaanalysis.TimelineTrace{
		Namespace: "prod",
		HPAName:   "web",
		Snapshots: []hpaanalysis.TimelineSnapshot{
			{Desired: 2},
			{Desired: 5},
			{Desired: 3},
			{Desired: 6},
			{Desired: 3},
		},
	}
	if err := writeRecordLine(tmp, trace); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := &options{}
	if err := runAnalyzeRecord(&buf, opts, tmp.Name(), "flapping"); err != nil {
		t.Fatalf("runAnalyzeRecord returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Detected HPA flapping") || !strings.Contains(output, "scale direction alternated") {
		t.Fatalf("expected flapping analysis, got:\n%s", output)
	}
}

func TestStatusHiddenFactorsText(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 3),
		kube.WithResourceMetric("cpu", 80, 84),
		kube.WithScalingLimitedTrue("TooManyReplicas"),
	)
	fakeClient := kube.NewFakeClient(hpa)
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
		statusOptions: statusOptions{
			events:        eventOption{enabled: false},
			hiddenFactors: true,
		},
	}

	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, opts, "web", true); err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Score Breakdown") || !strings.Contains(output, "Hidden decision factors") {
		t.Fatalf("expected score breakdown and hidden factors, got:\n%s", output)
	}
}

func TestStatusStructuredFormat(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(3, 5),
		kube.WithResourceMetric("cpu", 80, 120),
	)
	fakeClient := kube.NewFakeClient(hpa)
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
			format: "structured",
		},
	}

	var buf bytes.Buffer
	if err := runStatus(context.Background(), &buf, opts, "web", true); err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"schemaVersion": "v1"`) || !strings.Contains(output, `"decisionPath"`) {
		t.Fatalf("expected structured decision trace, got:\n%s", output)
	}
}

func TestWriteListCIReports(t *testing.T) {
	report := hpaanalysis.ListReport{Items: []hpaanalysis.ListItem{
		{Namespace: "prod", Name: "web", Health: "OK", HealthScore: 100},
		{Namespace: "prod", Name: "api", Health: "LIMITED", HealthScore: 70, Issue: "at maxReplicas"},
	}}

	var junit bytes.Buffer
	if err := writeListJUnit(&junit, report); err != nil {
		t.Fatalf("writeListJUnit returned error: %v", err)
	}
	if !strings.Contains(junit.String(), `failures="1"`) || !strings.Contains(junit.String(), "prod/api") {
		t.Fatalf("expected junit failure, got:\n%s", junit.String())
	}

	var sarif bytes.Buffer
	if err := writeListSARIF(&sarif, report); err != nil {
		t.Fatalf("writeListSARIF returned error: %v", err)
	}
	if !strings.Contains(sarif.String(), `"version": "2.1.0"`) || !strings.Contains(sarif.String(), "kubernetes://prod/horizontalpodautoscalers/api") {
		t.Fatalf("expected sarif result, got:\n%s", sarif.String())
	}
}

func TestRunTuneSuggest(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithMinMax(2, 10))
	fakeClient := kube.NewFakeClient(hpa)
	opts := &options{commonOptions: commonOptions{clientOverride: fakeClient}}

	var buf bytes.Buffer
	if err := runTune(context.Background(), &buf, opts, "web", "stable", true); err != nil {
		t.Fatalf("runTune returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "HPA Tuning Advisor") || !strings.Contains(output, "stabilizationWindowSeconds: 300") {
		t.Fatalf("expected tune advisor output, got:\n%s", output)
	}
}

func TestWriteAIContext(t *testing.T) {
	report := hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{
		Namespace:   "prod",
		Name:        "web",
		Target:      "Deployment/web",
		Current:     2,
		Desired:     4,
		Min:         2,
		Max:         10,
		Health:      "LIMITED",
		HealthScore: 75,
		Summary:     "Scaling up",
	}}
	var buf bytes.Buffer
	if err := writeAIContext(&buf, report, "why is it slow?"); err != nil {
		t.Fatalf("writeAIContext returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "# HPA AI Context") || !strings.Contains(output, "Question: why is it slow?") || !strings.Contains(output, "prod/web") {
		t.Fatalf("expected AI context output, got:\n%s", output)
	}
}
