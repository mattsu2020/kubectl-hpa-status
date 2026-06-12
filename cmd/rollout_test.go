package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

func TestNewRolloutCommand(t *testing.T) {
	opts := &options{}
	cmd := newRolloutCommand(opts)

	if cmd.Use != "rollout NAME [NAME...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "rollout") {
		t.Fatalf("unexpected Short: %q", cmd.Short)
	}
}

func TestRunRolloutJSONOutput(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(8, 12),
		kube.WithResourceMetric("cpu", 80, 90),
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

	err := runRollout(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runRollout returned error: %v", err)
	}

	var output rolloutOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if output.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", output.Namespace)
	}
	if output.Name != "web" {
		t.Errorf("expected name web, got %s", output.Name)
	}
	if output.Report == nil {
		t.Fatal("expected RolloutReport to be populated")
	}
	if len(output.Report.Checks) == 0 {
		t.Error("expected at least one check")
	}
}

func TestRunRolloutTextOutput(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(8, 12),
		kube.WithResourceMetric("cpu", 80, 90),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			output:         "",
			color:          "never",
		},
		statusOptions: statusOptions{
			events: eventOption{enabled: false},
		},
	}

	err := runRollout(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runRollout returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Rollout-aware HPA checks for HPA web/default") {
		t.Errorf("expected output to contain HPA header, got:\n%s", output)
	}
	if !strings.Contains(output, "Checks:") {
		t.Errorf("expected output to contain 'Checks:', got:\n%s", output)
	}
}

func TestRunRolloutNoRolloutInProgress(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithReplicas(5, 5),
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

	err := runRollout(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runRollout returned error: %v", err)
	}

	var output rolloutOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if output.Report == nil {
		t.Fatal("expected RolloutReport to be populated")
	}
	if output.Report.RolloutInProgress {
		t.Error("expected RolloutInProgress to be false")
	}
	// Without a Deployment in the fake client, probes cannot be detected,
	// so the summary may show check failures. Verify the report is structured.
	if len(output.Report.Checks) == 0 {
		t.Error("expected at least one check to be populated")
	}
}

func TestRootHelpIncludesRolloutCommand(t *testing.T) {
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root help returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "rollout") {
		t.Fatalf("expected root help to include rollout command, got:\n%s", buf.String())
	}
}
