package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestNewBlockersCommand(t *testing.T) {
	opts := &options{}
	cmd := newBlockersCommand(opts)

	if cmd.Use != "blockers NAME [NAME...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "scale-out") {
		t.Fatalf("unexpected Short: %q", cmd.Short)
	}
}

func TestRunBlockersBasicOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(8, 12),
		testutil.WithResourceMetric("cpu", 80, 90),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Output:         "json",
		Events:         EventOption{Enabled: false},
	}

	err := runBlockers(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runBlockers returned error: %v", err)
	}

	var output blockerOutput
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
		t.Fatal("expected BlockerReport to be populated")
	}
	if !output.Report.HPAWantsScale {
		t.Error("expected HPAWantsScale to be true when desired > current")
	}
	if output.Report.DesiredReplicas != 12 {
		t.Errorf("expected DesiredReplicas=12, got %d", output.Report.DesiredReplicas)
	}
	if len(output.Report.Blockers) == 0 {
		t.Error("expected at least one blocker finding")
	}
}

func TestRunBlockersNoScaleOut(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(5, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Output:         "json",
		Events:         EventOption{Enabled: false},
	}

	err := runBlockers(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runBlockers returned error: %v", err)
	}

	var output blockerOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if output.Report.HPAWantsScale {
		t.Error("expected HPAWantsScale to be false when desired == current")
	}
	if !strings.Contains(output.Report.Summary, "not actively requesting scale-out") {
		t.Errorf("expected summary about no scale-out, got %s", output.Report.Summary)
	}
}

func TestRunBlockersTextOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(8, 12),
		testutil.WithResourceMetric("cpu", 80, 90),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Output:         "",
		Color:          "never",
		Events:         EventOption{Enabled: false},
	}

	err := runBlockers(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runBlockers returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "HPA default/web") {
		t.Errorf("expected output to contain HPA header, got:\n%s", output)
	}
	if !strings.Contains(output, "Scale-out blockers") {
		t.Errorf("expected output to contain 'Scale-out blockers', got:\n%s", output)
	}
}

func TestCapacityDeepFlagOnDoctor(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		ClientOverride: fakeClient,
		Output:         "json",
		Events:         EventOption{Enabled: false},
		CapacityDeep:   true,
	}

	err := runDoctor(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runDoctor with --capacity-deep returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if report.Analysis.BlockerReport == nil {
		t.Fatal("expected BlockerReport to be populated with --capacity-deep")
	}
}

func TestRootHelpIncludesBlockersCommand(t *testing.T) {
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root help returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "blockers") {
		t.Fatalf("expected root help to include blockers command, got:\n%s", buf.String())
	}
}
