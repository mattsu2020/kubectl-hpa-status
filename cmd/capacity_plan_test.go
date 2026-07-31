package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
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
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
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
	hpaGets := 0
	for _, action := range fakeClient.Actions() {
		if action.Matches("get", "horizontalpodautoscalers") {
			hpaGets++
		}
	}
	if hpaGets != 1 {
		t.Fatalf("dedicated capacity path fetched the HPA %d times, want 1", hpaGets)
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
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "",
				Color:  "never",
			},
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
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
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
			ConnectionOptions: ConnectionOptions{
				ClientOverride: fakeClient,
			},
			OutputOptions: OutputOptions{
				Output: "json",
			},
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

func TestAssembleCapacityPlanInput_RecordsFetchErrorsAsUnknown(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithReplicas(5, 5),
		testutil.WithMinMax(1, 5),
	)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "app",
					Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					}},
				}}},
			},
		},
	}
	fakeClient := testutil.NewFakeClientWithObjects(hpa, deployment)
	fakeClient.PrependReactor("list", "resourcequotas", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	client := &kube.Client{Interface: fakeClient, Namespace: "default"}
	analysis := hpaanalysis.Analysis{
		Namespace: "default",
		Name:      "web",
		Target:    "Deployment/web",
		Current:   5,
		Max:       5,
	}

	input := assembleCapacityPlanInput(context.Background(), client, hpa, analysis, 10)
	plan := hpaanalysis.AnalyzeCapacityPlan(input)

	if plan.Safe {
		t.Fatal("ResourceQuota fetch failure must not produce a Safe plan")
	}
	foundUnknown := false
	for _, check := range plan.Checks {
		if check.Unknown && strings.Contains(check.Message, "ResourceQuotas unknown") {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("expected ResourceQuota fetch failure as unknown, got %+v", plan.Checks)
	}
}
