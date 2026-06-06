package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

func TestRunRecommend_WellConfiguredHPA(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 70, 65),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Target:") {
		t.Fatalf("expected 'Target:' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Compliance Score:") {
		t.Fatalf("expected 'Compliance Score:' in output, got:\n%s", output)
	}
}

func TestRunRecommend_PoorlyConfiguredHPA(t *testing.T) {
	hpa := kube.BuildHPA("default", "bad-hpa",
		kube.WithMinMax(0, 100),
		kube.WithResourceMetric("cpu", 95, 99),
	)
	// Set minReplicas to 0 to trigger scale-to-zero finding
	*hpa.Spec.MinReplicas = 0

	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"bad-hpa"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "warning") {
		t.Fatalf("expected warning in output for poorly configured HPA, got:\n%s", output)
	}
	// Score should be lower than 100 for a poorly configured HPA
	if strings.Contains(output, "100/100") {
		t.Fatalf("did not expect perfect score for poorly configured HPA, got:\n%s", output)
	}
}

func TestRunRecommend_NonExistentHPA(t *testing.T) {
	// Empty fake client — no HPAs registered.
	fakeClient := kube.NewFakeClient()

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent HPA, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected error to mention HPA name, got: %v", err)
	}
}

func TestRunRecommend_KEDAManagedHPA(t *testing.T) {
	hpa := kube.BuildHPA("default", "keda-worker",
		kube.WithKEDALabels("worker"),
		kube.WithExternalMetricWithStatus("queue-depth", "5", "10"),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"keda-worker"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "keda-managed") {
		t.Fatalf("expected keda-managed finding in output, got:\n%s", output)
	}
}

func TestRunRecommend_JSONOutput(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 70, 65),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient, output: "json"},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"score"`) {
		t.Fatalf("expected JSON with score field, got:\n%s", output)
	}
	if !strings.Contains(output, `"findings"`) {
		t.Fatalf("expected JSON with findings field, got:\n%s", output)
	}
	if !strings.Contains(output, `"name"`) {
		t.Fatalf("expected JSON with name field, got:\n%s", output)
	}
}

func TestRunRecommend_YAMLOutput(t *testing.T) {
	hpa := kube.BuildHPA("default", "web",
		kube.WithResourceMetric("cpu", 70, 65),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient, output: "yaml"},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "score:") {
		t.Fatalf("expected YAML with score field, got:\n%s", output)
	}
	if !strings.Contains(output, "findings:") {
		t.Fatalf("expected YAML with findings field, got:\n%s", output)
	}
}

func TestRunRecommend_ScoreToZeroWarning(t *testing.T) {
	hpa := kube.BuildHPA("default", "scale-to-zero",
		kube.WithMinMax(0, 5),
		kube.WithResourceMetric("cpu", 70, 50),
	)
	// Ensure minReplicas is 0
	*hpa.Spec.MinReplicas = 0

	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"scale-to-zero"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "scale-to-zero") {
		t.Fatalf("expected scale-to-zero finding in output, got:\n%s", output)
	}
}

func TestRunRecommend_ExtractsMinReplicasFromSpec(t *testing.T) {
	minReplicas := int32(3)
	hpa := kube.BuildHPA("default", "explicit-min",
		kube.WithMinMax(3, 10),
		kube.WithResourceMetric("cpu", 70, 65),
	)
	hpa.Spec.MinReplicas = &minReplicas

	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"explicit-min"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Target:") {
		t.Fatalf("expected output for HPA with explicit minReplicas, got:\n%s", output)
	}
}

func TestNewRecommendCommand(t *testing.T) {
	opts := &options{}
	cmd := newRecommendCommand(opts)
	if cmd.Use != "recommend NAME [NAME...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Fatal("expected non-empty Short description")
	}
}

func TestRunRecommend_UsesNamespaceFromOptions(t *testing.T) {
	hpa := kube.BuildHPA("team-a", "web",
		kube.WithResourceMetric("cpu", 70, 65),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{
			clientOverride: fakeClient,
			namespace:      "team-a",
		},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "team-a") {
		t.Fatalf("expected namespace team-a in output, got:\n%s", output)
	}
}

func TestRunRecommend_HighUtilizationWarning(t *testing.T) {
	hpa := kube.BuildHPA("default", "high-util",
		kube.WithResourceMetric("cpu", 95, 98),
	)
	fakeClient := kube.NewFakeClient(hpa)

	var buf bytes.Buffer
	opts := &options{
		commonOptions: commonOptions{clientOverride: fakeClient},
	}
	err := runRecommend(context.Background(), &buf, opts, []string{"high-util"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "High cpu target utilization") {
		t.Fatalf("expected high utilization warning in output, got:\n%s", output)
	}
}
