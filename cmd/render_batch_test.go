package cmd

import (
	"bytes"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// This file holds cross-cutting renderer smoke tests (write* helpers and the
// alerts command). They were split out of the former feature_batch_test.go
// grab-bag so each renderer's deeper tests live next to its source while these
// broad smoke checks stay grouped.

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
