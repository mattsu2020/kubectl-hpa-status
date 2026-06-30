//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	// Import the root command package from our plugin
	"github.com/mattsu2020/kubectl-hpa-status/cmd"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

func TestE2E_HPAStatus(t *testing.T) {
	// 1. Resolve kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home := os.Getenv("HOME")
		if home != "" {
			kubeconfig = home + "/.kube/config"
		}
	}

	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		t.Skipf("Skipping E2E test: kubeconfig file %q does not exist", kubeconfig)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Skipf("Skipping E2E test: failed to build config from kubeconfig %q: %v", kubeconfig, err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Skipf("Skipping E2E test: failed to create clientset: %v", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Discovery().ServerVersion()
	if err != nil {
		t.Skipf("Skipping E2E test: API server is unreachable: %v", err)
	}

	// 2. Create a temporary namespace
	nsName := fmt.Sprintf("hpa-status-e2e-%d", rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100000))
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace %s: %v", nsName, err)
	}
	defer func() {
		// Clean up namespace
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.CoreV1().Namespaces().Delete(cleanupCtx, nsName, metav1.DeleteOptions{})
	}()

	// 3. Create dummy Scale Target (e.g. dummy Deployment target metadata)
	// We don't need a real Deployment running pods if we mock HPA status, but we need
	// the target object metadata so that HPA targetRef is valid (though HPA controller won't run, API allows it).
	// Let's create a minimal Scale Target.
	// HPA requires a Scale subresource on target. We can target a dummy ReplicationController.
	rc := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rc",
			Namespace: nsName,
		},
		Spec: corev1.ReplicationControllerSpec{
			Replicas: int32Ptr(2),
			Selector: map[string]string{"app": "test"},
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "web",
							Image: "nginx:alpine",
						},
					},
				},
			},
		},
	}
	_, err = client.CoreV1().ReplicationControllers(nsName).Create(ctx, rc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ReplicationController: %v", err)
	}

	// 4. Create HPA
	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       "test-rc",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(80),
						},
					},
				},
			},
		},
	}
	hpa, err = client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create HPA: %v", err)
	}

	// 5. Update HPA Status (Simulating HPA Controller decisions)
	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 3,
		DesiredReplicas: 5,
		Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
			{
				Type:               autoscalingv2.ScalingActive,
				Status:             corev1.ConditionTrue,
				Reason:             "ValidMetricFound",
				Message:            "the HPA was able to successfully calculate a recommendation",
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               autoscalingv2.AbleToScale,
				Status:             corev1.ConditionTrue,
				Reason:             "ReadyForScale",
				Message:            "recommended size is different from current size",
				LastTransitionTime: metav1.Now(),
			},
		},
		CurrentMetrics: []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: int32Ptr(120),
					},
				},
			},
		},
	}

	_, err = client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update HPA status: %v", err)
	}

	// 6. Execute the plugin's commands via Cobra Root Command
	// We run 'status' command
	rootCmd := cmd.NewRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "test-hpa", "-n", nsName, "--no-interpret=false", "--explain=true", "--kubeconfig", kubeconfig})

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("failed to execute status command: %v. Output:\n%s", err, buf.String())
	}

	output := buf.String()
	t.Logf("CLI Output:\n%s", output)

	// Verify we got the expected interpretation and scaling metrics
	if !strings.Contains(output, "HPA currently wants to scale up") {
		t.Errorf("expected output to mention scale up, got:\n%s", output)
	}
	if !strings.Contains(output, "current=3 desired=5") {
		t.Errorf("expected output to contain replicas, got:\n%s", output)
	}
	if !strings.Contains(output, "Resource cpu current=120% target=80%") {
		t.Errorf("expected CPU metrics details, got:\n%s", output)
	}

	// Test the 'list' command
	buf.Reset()
	rootCmd = cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-n", nsName, "--kubeconfig", kubeconfig})

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("failed to execute list command: %v. Output:\n%s", err, buf.String())
	}

	listOutput := buf.String()
	t.Logf("List Output:\n%s", listOutput)
	if !strings.Contains(listOutput, "test-hpa") {
		t.Errorf("expected test-hpa in list output, got:\n%s", listOutput)
	}

	// Test Japanese labels stay wired through the command path.
	buf.Reset()
	rootCmd = cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "test-hpa", "-n", nsName, "--lang=ja", "--kubeconfig", kubeconfig})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute Japanese status command: %v. Output:\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "対象:") {
		t.Errorf("expected Japanese status labels, got:\n%s", buf.String())
	}

	// Test the cluster-wide problem scan command. This HPA is healthy enough to
	// produce no rows, but the command path and all-namespace list must succeed.
	buf.Reset()
	rootCmd = cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"scan", "--kubeconfig", kubeconfig})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute scan command: %v. Output:\n%s", err, buf.String())
	}
}

func TestE2E_JSONOutput(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	config, client, nsName := setupTestNamespace(t, kubeconfig)
	_ = config

	createTestRC(t, client, nsName, "test-rc")
	createHealthyHPA(t, client, nsName, "test-hpa", "test-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "test-hpa", "-n", nsName, "-o", "json", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute status -o json command: %v. Output:\n%s", err, buf.String())
	}

	raw := buf.String()
	t.Logf("JSON Output:\n%s", raw)

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}

	analysis, ok := result["analysis"].(map[string]any)
	if !ok {
		t.Fatal("JSON output missing top-level 'analysis' object")
	}

	if _, exists := analysis["health"]; !exists {
		t.Error("JSON analysis missing 'health' field")
	}
	if _, exists := analysis["healthScore"]; !exists {
		t.Error("JSON analysis missing 'healthScore' field")
	}
	metrics, ok := analysis["metrics"].([]any)
	if !ok {
		t.Error("JSON analysis missing 'metrics' array")
	} else if len(metrics) == 0 {
		t.Error("JSON analysis 'metrics' array is empty; expected at least one metric")
	}
}

func TestE2E_ScalingInactive(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	config, client, nsName := setupTestNamespace(t, kubeconfig)
	_ = config

	createTestRC(t, client, nsName, "test-rc")

	// Create a healthy HPA for baseline
	createHealthyHPA(t, client, nsName, "test-hpa", "test-rc")

	// Create a second RC + HPA with ScalingActive=False (broken HPA)
	createTestRC(t, client, nsName, "broken-rc")
	createBrokenHPA(t, client, nsName, "broken-hpa", "broken-rc")

	// Test status --explain on the broken HPA
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "broken-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		// The command may return a non-nil error (ExitCodeError) due to ERROR health.
		// This is expected behavior; exit codes are set for script integration.
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	explainOutput := buf.String()
	t.Logf("Broken HPA explain output:\n%s", explainOutput)

	if !strings.Contains(explainOutput, "ScalingActive") {
		t.Errorf("expected output to mention ScalingActive, got:\n%s", explainOutput)
	}

	// Test list --problem shows the broken HPA with ERROR health
	buf.Reset()
	rootCmd = cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-n", nsName, "--problem", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute list --problem: %v. Output:\n%s", err, buf.String())
	}

	problemOutput := buf.String()
	t.Logf("List --problem output:\n%s", problemOutput)

	if !strings.Contains(problemOutput, "broken-hpa") {
		t.Errorf("expected broken-hpa in --problem output, got:\n%s", problemOutput)
	}
	if !strings.Contains(problemOutput, "ERROR") {
		t.Errorf("expected ERROR health in --problem output, got:\n%s", problemOutput)
	}
}

func TestE2E_SuggestWorkflow(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	config, client, nsName := setupTestNamespace(t, kubeconfig)
	_ = config

	createTestRC(t, client, nsName, "test-rc")
	createHealthyHPA(t, client, nsName, "test-hpa", "test-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "test-hpa", "-n", nsName, "--suggest", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute status --suggest command: %v. Output:\n%s", err, buf.String())
	}

	suggestOutput := buf.String()
	t.Logf("Suggest Output:\n%s", suggestOutput)

	// The output should contain suggestion text - either a concrete suggestion or
	// the fallback "No safe automatic fix" message.
	if !strings.Contains(suggestOutput, "No safe automatic fix") &&
		!strings.Contains(suggestOutput, "Suggested") &&
		!strings.Contains(suggestOutput, "suggestion") &&
		!strings.Contains(suggestOutput, "Recommend") {
		t.Errorf("expected suggestion-related text in output, got:\n%s", suggestOutput)
	}
}

// ---------------------------------------------------------------------------
// Phase 4: Expanded E2E tests
// ---------------------------------------------------------------------------

// TestE2E_ScaleToZero verifies that status --explain detects scale-to-zero
// configuration when minReplicas=0 and both current/desired replicas are zero.
func TestE2E_ScaleToZero(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "zero-rc")
	if !createScaleToZeroHPA(t, client, nsName, "zero-hpa", "zero-rc") {
		t.Skip("Skipping: minReplicas=0 requires HPAScaleToZero feature gate (K8s >= 1.27 with feature gate enabled)")
	}

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "zero-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	output := buf.String()
	t.Logf("Scale-to-zero output:\n%s", output)

	// The interpret.go code produces "Scale-to-zero is enabled" text for minReplicas=0.
	if !strings.Contains(output, "scale-to-zero") && !strings.Contains(output, "cold start") && !strings.Contains(output, "scaled to zero") {
		t.Errorf("expected scale-to-zero or cold start mention in output, got:\n%s", output)
	}
}

// TestE2E_MultiMetricWinner verifies that status --explain reports metric
// impact estimation when an HPA has multiple resource metrics with different
// ratios, identifying the metric with the largest distance from target.
func TestE2E_MultiMetricWinner(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "multi-rc")
	createMultiMetricHPA(t, client, nsName, "multi-hpa", "multi-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "multi-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	output := buf.String()
	t.Logf("Multi-metric output:\n%s", output)

	// The interpret.go code produces "largest distance from target" or
	// "largest visible ratio distance" when multiple metrics are present.
	if !strings.Contains(output, "largest distance") &&
		!strings.Contains(output, "largest visible ratio distance") &&
		!strings.Contains(output, "MetricImpactEstimate") &&
		!strings.Contains(output, "Multiple current metrics") {
		t.Errorf("expected multi-metric impact estimation in output, got:\n%s", output)
	}

	// Stronger schema contract: decode JSON and assert the winner metric is
	// CPU (150% vs 80% target = 1.875 ratio beats memory 75% vs 70% = 1.07).
	multiRaw := runStatusJSON(t, kubeconfig, nsName, "multi-hpa", "--explain")
	assertStatusReportShape(t, multiRaw, "multi-hpa")
	multiReport := decodeStatusReport(t, multiRaw)
	if multiReport.Analysis.ImpactMetric == nil {
		t.Fatal("expected Analysis.ImpactMetric to be populated for a multi-metric HPA")
	}
	if multiReport.Analysis.ImpactMetric.Name != "cpu" {
		t.Errorf("expected winner metric cpu, got %q", multiReport.Analysis.ImpactMetric.Name)
	}
	if len(multiReport.Analysis.Metrics) < 2 {
		t.Errorf("expected at least 2 metrics in Analysis.Metrics, got %d", len(multiReport.Analysis.Metrics))
	}
}

// TestE2E_StabilizationWindow verifies that status --explain reports
// scale-down stabilization when the AbleToScale condition reason is
// ScaleDownStabilized and a stabilization window is configured.
func TestE2E_StabilizationWindow(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "stabilized-rc")
	createStabilizedHPA(t, client, nsName, "stabilized-hpa", "stabilized-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "stabilized-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	output := buf.String()
	t.Logf("Stabilization output:\n%s", output)

	if !strings.Contains(output, "stabilized") && !strings.Contains(output, "remaining") {
		t.Errorf("expected stabilized or remaining mention in output, got:\n%s", output)
	}

	// Stronger schema contract: decode JSON and assert the stabilization
	// window and source match the configured behavior (300s scaleDown).
	stabRaw := runStatusJSON(t, kubeconfig, nsName, "stabilized-hpa", "--explain")
	assertStatusReportShape(t, stabRaw, "stabilized-hpa")
	stabReport := decodeStatusReport(t, stabRaw)
	a := stabReport.Analysis
	if a.StabilizationWindowSeconds == nil {
		t.Fatal("expected Analysis.StabilizationWindowSeconds to be populated for a stabilized HPA")
	}
	if *a.StabilizationWindowSeconds != 300 {
		t.Errorf("StabilizationWindowSeconds = %d, want 300", *a.StabilizationWindowSeconds)
	}
	if a.StabilizationSource != "scaleDown" {
		t.Errorf("StabilizationSource = %q, want %q", a.StabilizationSource, "scaleDown")
	}
	if a.StabilizationConfidence == "" {
		t.Error("StabilizationConfidence is empty; expected a confidence label for stabilization estimates")
	}
}

// ---------------------------------------------------------------------------
// Phase 5: Expanded problem scenarios and output schema validation (#9)
// ---------------------------------------------------------------------------

// TestE2E_ExternalMetricFailure verifies that an HPA whose ScalingActive=False
// reason is FailedGetResourceMetric while the spec references an External
// metric source is correctly surfaced through status --explain and list
// --problem. This exercises the custom/external metrics failure path that
// differs from the plain resource-metric broken HPA.
func TestE2E_ExternalMetricFailure(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "ext-rc")
	createBrokenExternalMetricHPA(t, client, nsName, "ext-hpa", "ext-rc")

	// status --explain should report ScalingActive failure details and the
	// external metric remediation hint.
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "ext-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		// ERROR health yields a non-nil ExitCodeError, which is expected.
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	explainOutput := buf.String()
	t.Logf("External metric failure explain output:\n%s", explainOutput)

	if !strings.Contains(explainOutput, "ScalingActive") {
		t.Errorf("expected output to mention ScalingActive, got:\n%s", explainOutput)
	}
	if !strings.Contains(explainOutput, "FailedGetResourceMetric") {
		t.Errorf("expected output to mention FailedGetResourceMetric reason, got:\n%s", explainOutput)
	}
	// The remediation hint for external metrics should reference the metric name.
	if !strings.Contains(explainOutput, "queue-depth") {
		t.Errorf("expected external metric name 'queue-depth' in output, got:\n%s", explainOutput)
	}

	// list --problem should classify this HPA as ERROR.
	buf.Reset()
	rootCmd = cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-n", nsName, "--problem", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute list --problem: %v. Output:\n%s", err, buf.String())
	}

	problemOutput := buf.String()
	t.Logf("External metric list --problem output:\n%s", problemOutput)

	if !strings.Contains(problemOutput, "ext-hpa") {
		t.Errorf("expected ext-hpa in --problem output, got:\n%s", problemOutput)
	}
	if !strings.Contains(problemOutput, "ERROR") {
		t.Errorf("expected ERROR health in --problem output, got:\n%s", problemOutput)
	}
}

// TestE2E_TooFewReplicas verifies that an HPA sitting at minReplicas with
// desired==current==minReplicas is described with the at-minReplicas summary.
func TestE2E_TooFewReplicas(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "toofew-rc")
	createTooFewReplicasHPA(t, client, nsName, "toofew-hpa", "toofew-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "toofew-hpa", "-n", nsName, "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	output := buf.String()
	t.Logf("Too-few-replicas output:\n%s", output)

	// The summary line for desired==current==minReplicas is "HPA is at minReplicas."
	if !strings.Contains(output, "at minReplicas") {
		t.Errorf("expected 'at minReplicas' summary text in output, got:\n%s", output)
	}
	if !strings.Contains(output, "minReplicas") {
		t.Errorf("expected minReplicas mention in output, got:\n%s", output)
	}
}

// TestE2E_JSONTypedDecode verifies that status -o json output decodes into the
// typed hpaanalysis.StatusReport / Analysis structs without error and that the
// primary scalar and slice fields are populated with expected values. This is a
// stronger contract check than the loose map[string]any decode used by
// TestE2E_JSONOutput / TestE2E_JSONStructuredOutput.
func TestE2E_JSONTypedDecode(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "typed-rc")
	createHealthyHPA(t, client, nsName, "typed-hpa", "typed-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "typed-hpa", "-n", nsName, "-o", "json", "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	raw := buf.String()
	t.Logf("JSON typed-decode output:\n%s", raw)

	var report hpaanalysis.StatusReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("output does not decode into hpaanalysis.StatusReport: %v\nraw:\n%s", err, raw)
	}

	a := report.Analysis

	// Identity fields must round-trip exactly.
	if a.Name != "typed-hpa" {
		t.Errorf("expected Analysis.Name=%q, got %q", "typed-hpa", a.Name)
	}
	if a.Namespace != nsName {
		t.Errorf("expected Analysis.Namespace=%q, got %q", nsName, a.Namespace)
	}

	// Health must be a non-empty known state.
	switch a.Health {
	case "OK", "ERROR", "LIMITED", "STABILIZED":
		// ok
	default:
		t.Errorf("expected Analysis.Health to be a known state, got %q", a.Health)
	}

	// HealthScore must be in [0, 100].
	if a.HealthScore < 0 || a.HealthScore > 100 {
		t.Errorf("expected HealthScore in [0,100], got %d", a.HealthScore)
	}

	// The healthy HPA fixture has one CPU resource metric.
	if len(a.Metrics) == 0 {
		t.Error("expected at least one Analysis.Metrics entry, got empty slice")
	} else {
		m := a.Metrics[0]
		if m.Type != "Resource" {
			t.Errorf("expected first metric Type=%q, got %q", "Resource", m.Type)
		}
		if m.Text == "" {
			t.Error("expected non-empty Metric.Text")
		}
	}

	// The healthy HPA fixture reports current=3, desired=5.
	if a.Current != 3 {
		t.Errorf("expected Analysis.Current=3, got %d", a.Current)
	}
	if a.Desired != 5 {
		t.Errorf("expected Analysis.Desired=5, got %d", a.Desired)
	}

	// --explain populates the structured interpretation.
	if len(a.StructuredInterpretation) == 0 {
		t.Error("expected non-empty StructuredInterpretation from --explain")
	}
}

// TestE2E_JapaneseOutput verifies that --lang=ja produces Japanese label and
// direction strings. Time-dependent lines are avoided by asserting only on
// stable strings (labels and the scale-up direction sentence).
func TestE2E_JapaneseOutput(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "ja-rc")
	createHealthyHPA(t, client, nsName, "ja-hpa", "ja-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "ja-hpa", "-n", nsName, "--lang=ja", "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	output := buf.String()
	t.Logf("Japanese output:\n%s", output)

	// Stable label strings that do not depend on the cluster clock.
	for _, want := range []string{"対象", "レプリカ", "状態", "メトリクス"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected Japanese label %q in output, got:\n%s", want, output)
		}
	}
	// The healthy HPA recommends scale-up (desired=5 > current=3), so the
	// Japanese scale-up direction sentence should appear verbatim.
	if !strings.Contains(output, "HPAは現在スケールアップを希望しています。") {
		t.Errorf("expected Japanese scale-up direction sentence in output, got:\n%s", output)
	}
}
