package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestNewCapacityPlanCommand(t *testing.T) {
	opts := &options{}
	cmd := newCapacityPlanCommand(opts)

	if cmd.Use != "capacity NAME [NAME...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "maxReplicas") {
		t.Fatalf("unexpected Short: %q", cmd.Short)
	}
}

func TestRunCapacityPlan_JSONOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithMinMax(5, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}

	err := runCapacityPlan(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runCapacityPlan returned error: %v", err)
	}

	var output capacityPlanOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if output.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %s", output.Namespace)
	}
	if output.Name != "web" {
		t.Errorf("expected name 'web', got %s", output.Name)
	}
	if output.Plan == nil {
		t.Fatal("expected CapacityPlan to be populated")
	}
	if output.Plan.MaxReplicas != 10 {
		t.Errorf("expected maxReplicas 10, got %d", output.Plan.MaxReplicas)
	}
	if output.Plan.TargetMaxReplicas <= 0 {
		t.Errorf("expected positive targetMaxReplicas, got %d", output.Plan.TargetMaxReplicas)
	}
}

func TestRunCapacityPlan_TextOutput(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithMinMax(5, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "",
			Color:          "never",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
		},
	}

	err := runCapacityPlan(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runCapacityPlan returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Capacity plan for") {
		t.Errorf("expected output to contain 'Capacity plan for', got:\n%s", output)
	}
	if !strings.Contains(output, "maxReplicas") {
		t.Errorf("expected output to contain 'maxReplicas', got:\n%s", output)
	}
	if !strings.Contains(output, "Checks:") {
		t.Errorf("expected output to contain 'Checks:', got:\n%s", output)
	}
	if !strings.Contains(output, "Recommendation:") {
		t.Errorf("expected output to contain 'Recommendation:', got:\n%s", output)
	}
}

func TestRunCapacityPlan_TargetMaxOverride(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithMinMax(5, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events:    EventOption{Enabled: false},
			TargetMax: 30,
		},
	}

	err := runCapacityPlan(context.Background(), &buf, opts, []string{"web"})
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runCapacityPlan returned error: %v", err)
	}

	var output capacityPlanOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if output.Plan.TargetMaxReplicas != 30 {
		t.Errorf("expected targetMaxReplicas 30, got %d", output.Plan.TargetMaxReplicas)
	}
	if output.Plan.AdditionalPods != 20 {
		t.Errorf("expected additionalPods 20, got %d", output.Plan.AdditionalPods)
	}
}

func TestRootHelpIncludesCapacityCommand(t *testing.T) {
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root help returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "capacity") {
		t.Fatalf("expected root help to include capacity command, got:\n%s", buf.String())
	}
}

func TestCapacityPlanFlagOnStatus(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(10, 10),
		testutil.WithResourceMetric("cpu", 80, 90),
		testutil.WithMinMax(5, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				CapacityPlan: true,
				Interpret:    true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", true)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus with --capacity-plan returned error: %v", err)
	}

	var report struct {
		Analysis struct {
			CapacityPlan *struct {
				TargetMaxReplicas int `json:"targetMaxReplicas"`
				Checks            []struct {
					Pass    bool   `json:"pass"`
					Message string `json:"message"`
				} `json:"checks"`
			} `json:"capacityPlan"`
		} `json:"analysis"`
	}
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf.String())
	}

	if report.Analysis.CapacityPlan == nil {
		t.Fatal("expected CapacityPlan to be populated with --capacity-plan flag")
	}
}
