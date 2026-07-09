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

func TestParseSimulateOverrides(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "single override",
			input: []string{"maxReplicas=20"},
			want:  map[string]string{"maxReplicas": "20"},
		},
		{
			name:  "multiple overrides",
			input: []string{"maxReplicas=20", "minReplicas=3"},
			want:  map[string]string{"maxReplicas": "20", "minReplicas": "3"},
		},
		{
			name:    "no equals sign",
			input:   []string{"maxReplicas"},
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   []string{"=20"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSimulateOverrides(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d overrides, got %d", len(tt.want), len(got))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("override[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// --capacity-context integration tests
// --------------------------------------------------------------------------

func TestRunStatus_ExplainPods(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events: EventOption{Enabled: false},
			Features: featuresOptions{
				ExplainPods: true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "web") {
		t.Error("expected output to contain HPA name")
	}
}

func TestRunStatus_ExplainPods_JSON(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
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
				ExplainPods: true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	_ = report.Analysis.PodAnalysis
}

// --------------------------------------------------------------------------
// --simulate integration tests
// --------------------------------------------------------------------------

func TestRunStatus_Simulate(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 70),
		testutil.WithMinMax(1, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
			Output:         "json",
		},
		Status: statusOptions{
			Events:   EventOption{Enabled: false},
			Simulate: []string{"maxReplicas=20"},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if report.Analysis.FlappingSimulation == nil {
		t.Fatal("expected Simulation to be populated")
	}
	if report.Analysis.FlappingSimulation.Parameter != "maxReplicas" {
		t.Errorf("expected parameter=maxReplicas, got %q", report.Analysis.FlappingSimulation.Parameter)
	}
	if report.Analysis.FlappingSimulation.OriginalValue != "10" {
		t.Errorf("expected originalValue=10, got %q", report.Analysis.FlappingSimulation.OriginalValue)
	}
	if report.Analysis.FlappingSimulation.SimulatedValue != "20" {
		t.Errorf("expected simulatedValue=20, got %q", report.Analysis.FlappingSimulation.SimulatedValue)
	}
}

func TestRunStatus_SimulateText(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 3),
		testutil.WithResourceMetric("cpu", 80, 70),
		testutil.WithMinMax(1, 10),
	)
	fakeClient := testutil.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		Common: commonOptions{
			ClientOverride: fakeClient,
		},
		Status: statusOptions{
			Events:   EventOption{Enabled: false},
			Simulate: []string{"maxReplicas=20"},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil && !isExitCodeWarning(err) {
		t.Fatalf("runStatus returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Simulation") {
		t.Error("expected output to contain 'Simulation' section")
	}
	if !strings.Contains(output, "maxReplicas") {
		t.Error("expected output to contain 'maxReplicas'")
	}
}

func TestRunStatus_CapacityContext(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(3, 5),
		testutil.WithResourceMetric("cpu", 80, 70),
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
				CapacityContext: true,
			},
		},
	}

	err := runStatus(context.Background(), &buf, opts, "web", false)
	if err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if report.Analysis.CapacityContext == nil {
		t.Error("expected CapacityContext to be populated")
	}
}
