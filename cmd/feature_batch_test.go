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
	if err := runEstimate(context.Background(), &buf, opts, "web", 30, 0.12); err != nil {
		t.Fatalf("runEstimate returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Additional worst-case pods: 20") || !strings.Contains(output, "$2.40/hour") {
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
	defer tmp.Close()

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
