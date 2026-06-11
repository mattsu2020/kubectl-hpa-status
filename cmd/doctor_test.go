package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestNewDoctorCommand(t *testing.T) {
	opts := &options{}
	cmd := newDoctorCommand(opts)

	if cmd.Use != "doctor NAME [NAME...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "Diagnose HPA scaling failures") {
		t.Fatalf("unexpected Short: %q", cmd.Short)
	}
}

func TestRunDoctorEnablesBundledDiagnostics(t *testing.T) {
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
			events: eventOption{enabled: false},
		},
	}

	err := runDoctor(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runDoctor returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if report.Analysis.MetricsDiagnostics == nil {
		t.Fatal("expected MetricsDiagnostics to be populated")
	}
	if len(report.Analysis.MetricFreshnessEntries) == 0 {
		t.Fatal("expected MetricFreshnessEntries to be populated")
	}
	if report.Analysis.CapacityContext == nil {
		t.Fatal("expected CapacityContext to be populated")
	}
	if len(report.Analysis.Interpretation) == 0 {
		t.Fatal("expected Interpretation to be populated")
	}
}

func TestRootHelpIncludesDoctorCommand(t *testing.T) {
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root help returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "doctor") {
		t.Fatalf("expected root help to include doctor command, got:\n%s", buf.String())
	}
}

func TestNewContextCommands(t *testing.T) {
	opts := &options{}
	nodeCmd := newNodeContextCommand(opts)
	if nodeCmd.Use != "node-context NAME [NAME...]" {
		t.Fatalf("unexpected node-context Use: %q", nodeCmd.Use)
	}
	rolloutCmd := newRolloutContextCommand(opts)
	if rolloutCmd.Use != "rollout-context NAME [NAME...]" {
		t.Fatalf("unexpected rollout-context Use: %q", rolloutCmd.Use)
	}
	containerCmd := newContainerAdvisorCommand(opts)
	if containerCmd.Use != "container-advisor NAME [NAME...]" {
		t.Fatalf("unexpected container-advisor Use: %q", containerCmd.Use)
	}
	gapCmd := newCapacityGapCommand(opts)
	if gapCmd.Use != "capacity-gap NAME [NAME...]" {
		t.Fatalf("unexpected capacity-gap Use: %q", gapCmd.Use)
	}
}

func TestPolicyInitProductionAPI(t *testing.T) {
	var buf bytes.Buffer
	if err := writePolicyProfile(&buf, "production-api"); err != nil {
		t.Fatalf("writePolicyProfile returned error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "apiVersion: hpa-status/v1") ||
		!strings.Contains(output, "metric-coverage") ||
		!strings.Contains(output, "target-utilization-range") {
		t.Fatalf("expected production policy profile, got:\n%s", output)
	}
}
