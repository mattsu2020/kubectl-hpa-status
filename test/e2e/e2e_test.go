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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Import the root command package from our plugin
	"github.com/mattsu2020/kubectl-hpa-status/cmd"
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

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}

	analysis, ok := result["analysis"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON output missing top-level 'analysis' object")
	}

	if _, exists := analysis["health"]; !exists {
		t.Error("JSON analysis missing 'health' field")
	}
	if _, exists := analysis["healthScore"]; !exists {
		t.Error("JSON analysis missing 'healthScore' field")
	}
	metrics, ok := analysis["metrics"].([]interface{})
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

// resolveKubeconfig finds the kubeconfig path or skips the test.
func resolveKubeconfig(t *testing.T) string {
	t.Helper()
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
	return kubeconfig
}

// setupTestNamespace creates a fresh namespace and returns the config, client, and namespace name.
// It registers cleanup to delete the namespace when the test finishes.
func setupTestNamespace(t *testing.T, kubeconfig string) (*rest.Config, *kubernetes.Clientset, string) {
	t.Helper()

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Skipf("Skipping E2E test: failed to build config from kubeconfig %q: %v", kubeconfig, err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Skipf("Skipping E2E test: failed to create clientset: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Discovery().ServerVersion(); err != nil {
		t.Skipf("Skipping E2E test: API server is unreachable: %v", err)
	}

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
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = client.CoreV1().Namespaces().Delete(cleanupCtx, nsName, metav1.DeleteOptions{})
	})

	return config, client, nsName
}

// createTestRC creates a minimal ReplicationController as a scale target for HPA.
func createTestRC(t *testing.T, client *kubernetes.Clientset, nsName, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: nsName,
		},
		Spec: corev1.ReplicationControllerSpec{
			Replicas: int32Ptr(2),
			Selector: map[string]string{"app": name},
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
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
	if _, err := client.CoreV1().ReplicationControllers(nsName).Create(ctx, rc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create ReplicationController %s: %v", name, err)
	}
}

// createHealthyHPA creates an HPA with ScalingActive=True and a CPU metric above target.
func createHealthyHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
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
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create HPA %s: %v", hpaName, err)
	}

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
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update HPA %s status: %v", hpaName, err)
	}
}

// createBrokenHPA creates an HPA with ScalingActive=False to simulate a metrics failure.
func createBrokenHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
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
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create broken HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 2,
		DesiredReplicas: 2,
		Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
			{
				Type:               autoscalingv2.ScalingActive,
				Status:             corev1.ConditionFalse,
				Reason:             "FailedGetResourceMetric",
				Message:            "unable to get metrics for resource cpu",
				LastTransitionTime: metav1.Now(),
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update broken HPA %s status: %v", hpaName, err)
	}
}

func int32Ptr(v int32) *int32 {
	return &v
}

// TestE2E_KEDAManagedHPA tests KEDA-managed HPA detection and display.
// This test creates an HPA with KEDA labels and verifies that the --keda flag
// produces KEDA-related output. The ScaledObject CRD lookup is expected to fail
// gracefully when KEDA is not installed.
func TestE2E_KEDAManagedHPA(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "keda-rc")

	// Create an HPA with KEDA labels (auto-detected as KEDA-managed)
	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-hpa-test",
			Namespace: nsName,
			Labels: map[string]string{
				"scaledobject.keda.sh/name": "test-scaledobject",
			},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       "keda-rc",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{
							Name: "queue_length",
						},
						Target: autoscalingv2.MetricTarget{
							Type:  autoscalingv2.AverageValueMetricType,
							Value: resourcePtr("5"),
						},
					},
				},
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create KEDA HPA: %v", err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 2,
		DesiredReplicas: 3,
		Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
			{
				Type:               autoscalingv2.ScalingActive,
				Status:             corev1.ConditionTrue,
				Reason:             "ValidMetricFound",
				Message:            "the HPA was able to successfully calculate a recommendation",
				LastTransitionTime: metav1.Now(),
			},
		},
		CurrentMetrics: []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricStatus{
					Metric: autoscalingv2.MetricIdentifier{
						Name: "queue_length",
					},
					Current: autoscalingv2.MetricValueStatus{
						AverageValue: resourcePtr("8"),
					},
				},
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update KEDA HPA status: %v", err)
	}

	// Test status --keda on the KEDA-labeled HPA
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "keda-hpa-test", "-n", nsName, "--keda", "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Logf("status --keda returned error (may be expected if KEDA CRD absent): %v", err)
	}

	output := buf.String()
	t.Logf("KEDA status output:\n%s", output)

	// Even without KEDA CRD, the output should detect KEDA labels
	if !strings.Contains(output, "KEDA") {
		t.Errorf("expected KEDA detection in output, got:\n%s", output)
	}
}

// TestE2E_ListApplyDryRun tests batch apply workflow with multiple HPAs.
func TestE2E_ListApplyDryRun(t *testing.T) {
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	// Create two RC+HPA pairs
	createTestRC(t, client, nsName, "apply-rc-1")
	createTestRC(t, client, nsName, "apply-rc-2")

	// Create an HPA at maxReplicas (ScalingLimited scenario)
	createScalingLimitedHPA(t, client, nsName, "limited-hpa-1", "apply-rc-1")
	createScalingLimitedHPA(t, client, nsName, "limited-hpa-2", "apply-rc-2")

	// Test list --problem --apply (dry-run by default)
	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-n", nsName, "--problem", "--fix", "--apply", "--yes", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Logf("list --problem --apply returned error: %v. Output:\n%s", err, buf.String())
	}

	output := buf.String()
	t.Logf("List apply output:\n%s", output)

	// Should show dry-run validation or suggestion output
	if !strings.Contains(output, "limited-hpa-1") && !strings.Contains(output, "No applicable") {
		t.Errorf("expected limited-hpa-1 in output, got:\n%s", output)
	}
}

// TestE2E_TUICommand verifies the tui command can be constructed without error.
// Full TUI testing requires a terminal, so we only verify the command path.
func TestE2E_TUICommand(t *testing.T) {
	rootCmd := cmd.NewRootCommand()

	// Verify the tui subcommand exists
	tuiCmd, _, err := rootCmd.Find([]string{"tui"})
	if err != nil {
		t.Fatalf("tui subcommand not found: %v", err)
	}
	if tuiCmd == nil {
		t.Fatal("tui subcommand is nil")
	}
	if tuiCmd.Short == "" {
		t.Error("tui subcommand missing Short description")
	}
}

// createScalingLimitedHPA creates an HPA that is at maxReplicas to trigger ScalingLimited.
func createScalingLimitedHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(1)
	maxReplicas := int32(3)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(50),
						},
					},
				},
			},
		},
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: maxReplicas,
		DesiredReplicas: maxReplicas,
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
				Message:            "recommended size matches current size",
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               autoscalingv2.ScalingLimited,
				Status:             corev1.ConditionTrue,
				Reason:             "TooManyReplicas",
				Message:            "the desired replica count is more than the maximum replica count",
				LastTransitionTime: metav1.Now(),
			},
		},
		CurrentMetrics: []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: int32Ptr(95),
					},
				},
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update HPA %s status: %v", hpaName, err)
	}
}

// resourcePtr creates a pointer to a resource.Quantity parsed from a string.
func resourcePtr(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
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
	createScaleToZeroHPA(t, client, nsName, "zero-hpa", "zero-rc")

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
}

// TestE2E_WatchCommand verifies that the watch command runs with a short
// timeout and completes. A context deadline exceeded error is expected.
func TestE2E_WatchCommand(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "watch-rc")
	createHealthyHPA(t, client, nsName, "watch-hpa", "watch-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "watch-hpa", "-n", nsName, "--watch", "--timeout", "3s", "--interval", "1s", "--kubeconfig", kubeconfig})

	err := rootCmd.Execute()
	// The watch command is expected to terminate via context timeout.
	// Either a nil error (graceful exit) or a context.DeadlineExceeded is acceptable.
	if err != nil {
		t.Logf("Watch command returned error (expected on timeout): %v", err)
	}

	output := buf.String()
	t.Logf("Watch output (first 500 chars):\n%s", firstN(output, 500))

	// Verify at least one status output was produced.
	if !strings.Contains(output, "watch-hpa") {
		t.Errorf("expected watch-hpa in output, got:\n%s", firstN(output, 500))
	}
}

// TestE2E_MultiNamespace verifies that list -A shows HPAs from multiple
// namespaces simultaneously.
func TestE2E_MultiNamespace(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName1 := setupTestNamespace(t, kubeconfig)
	_, client2, nsName2 := setupTestNamespace(t, kubeconfig)
	_ = client2

	createTestRC(t, client, nsName1, "multi-ns-rc-1")
	createHealthyHPA(t, client, nsName1, "multi-ns-hpa-1", "multi-ns-rc-1")

	createTestRC(t, client, nsName2, "multi-ns-rc-2")
	createHealthyHPA(t, client, nsName2, "multi-ns-hpa-2", "multi-ns-rc-2")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"list", "-A", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("failed to execute list -A: %v. Output:\n%s", err, buf.String())
	}

	output := buf.String()
	t.Logf("Multi-namespace list output:\n%s", output)

	if !strings.Contains(output, "multi-ns-hpa-1") {
		t.Errorf("expected multi-ns-hpa-1 in output, got:\n%s", output)
	}
	if !strings.Contains(output, "multi-ns-hpa-2") {
		t.Errorf("expected multi-ns-hpa-2 in output, got:\n%s", output)
	}
}

// TestE2E_JSONStructuredOutput verifies that status -o json --explain
// produces valid JSON with structuredInterpretation, suggestions, and
// numeric healthScore fields.
func TestE2E_JSONStructuredOutput(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "json-struct-rc")
	createHealthyHPA(t, client, nsName, "json-struct-hpa", "json-struct-rc")

	buf := new(bytes.Buffer)
	rootCmd := cmd.NewRootCommand()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "json-struct-hpa", "-n", nsName, "-o", "json", "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("unexpected error (not ExitCodeError): %v. Output:\n%s", err, buf.String())
		}
	}

	raw := buf.String()
	t.Logf("JSON Structured Output:\n%s", raw)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}

	analysis, ok := result["analysis"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON output missing top-level 'analysis' object")
	}

	// Verify structuredInterpretation array is present.
	structuredInterp, ok := analysis["structuredInterpretation"].([]interface{})
	if !ok {
		t.Error("JSON analysis missing 'structuredInterpretation' array")
	} else if len(structuredInterp) == 0 {
		t.Error("JSON analysis 'structuredInterpretation' array is empty")
	}

	// Verify suggestions array exists.
	suggestions, ok := analysis["suggestions"].([]interface{})
	if !ok {
		t.Error("JSON analysis missing 'suggestions' array")
	} else {
		t.Logf("Found %d suggestions", len(suggestions))
	}

	// Verify healthScore is numeric.
	healthScore, ok := analysis["healthScore"].(float64)
	if !ok {
		t.Errorf("JSON analysis 'healthScore' is not numeric, got: %T", analysis["healthScore"])
	} else {
		t.Logf("Health score: %.0f", healthScore)
		if healthScore < 0 || healthScore > 100 {
			t.Errorf("healthScore %.0f is outside expected range [0, 100]", healthScore)
		}
	}
}

// TestE2E_ConfigFile verifies that a custom config file with non-default
// health weights produces a different health score compared to default weights.
func TestE2E_ConfigFile(t *testing.T) {
	t.Parallel()
	kubeconfig := resolveKubeconfig(t)
	_, client, nsName := setupTestNamespace(t, kubeconfig)

	createTestRC(t, client, nsName, "config-rc")
	// Use a broken HPA so that health weights have a visible effect.
	createBrokenHPA(t, client, nsName, "config-hpa", "config-rc")

	// Step 1: Get the default health score.
	defaultBuf := new(bytes.Buffer)
	defaultCmd := cmd.NewRootCommand()
	defaultCmd.SetOut(defaultBuf)
	defaultCmd.SetErr(defaultBuf)
	defaultCmd.SetArgs([]string{"status", "config-hpa", "-n", nsName, "-o", "json", "--kubeconfig", kubeconfig})

	if err := defaultCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("default command error: %v", err)
		}
	}

	defaultScore := parseHealthScore(t, defaultBuf.String())
	t.Logf("Default health score: %d", defaultScore)

	// Step 2: Get the score with a custom config that sets scalingInactive to 10 (much lower penalty).
	configContent := `healthWeights:
  scalingInactive: 10
`
	configPath := writeTempConfigFile(t, configContent)

	configBuf := new(bytes.Buffer)
	configCmd := cmd.NewRootCommand()
	configCmd.SetOut(configBuf)
	configCmd.SetErr(configBuf)
	configCmd.SetArgs([]string{"status", "config-hpa", "-n", nsName, "-o", "json", "--config", configPath, "--kubeconfig", kubeconfig})

	if err := configCmd.Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("config command error: %v", err)
		}
	}

	configScore := parseHealthScore(t, configBuf.String())
	t.Logf("Custom config health score: %d", configScore)

	// The custom config sets scalingInactive=10 instead of the default 45,
	// so the health score should be higher (less penalty).
	if configScore <= defaultScore {
		t.Errorf("expected config score (%d) to be greater than default score (%d) with reduced scalingInactive penalty", configScore, defaultScore)
	}
}

// ---------------------------------------------------------------------------
// Phase 4: Helper functions
// ---------------------------------------------------------------------------

// createScaleToZeroHPA creates an HPA with minReplicas=0, CPU metric, and
// status indicating zero current and desired replicas (scale-to-zero state).
func createScaleToZeroHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(0)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
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
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create scale-to-zero HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 0,
		DesiredReplicas: 0,
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
				Message:            "recommended size is 0 replicas",
				LastTransitionTime: metav1.Now(),
			},
		},
		CurrentMetrics: []autoscalingv2.MetricStatus{},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update scale-to-zero HPA %s status: %v", hpaName, err)
	}
}

// createStabilizedHPA creates an HPA with AbleToScale condition reason
// ScaleDownStabilized, a recent LastScaleTime, and a configured
// scale-down stabilization window in behavior.
func createStabilizedHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(2)
	stabilizationWindow := int32(300) // 5 minutes
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
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
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &stabilizationWindow,
				},
			},
		},
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create stabilized HPA %s: %v", hpaName, err)
	}

	recentScaleTime := metav1.Now()
	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 4,
		DesiredReplicas: 4,
		LastScaleTime:   &recentScaleTime,
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
				Reason:             "ScaleDownStabilized",
				Message:            "recent recommendations were lower than current one, applying the highest recent recommendation",
				LastTransitionTime: metav1.Now(),
			},
		},
		CurrentMetrics: []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: int32Ptr(40),
					},
				},
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update stabilized HPA %s status: %v", hpaName, err)
	}
}

// createMultiMetricHPA creates an HPA with both CPU and Memory resource
// metrics with different ratios so that the metric impact estimation
// can identify the metric with the largest distance from target.
func createMultiMetricHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: nsName,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "v1",
				Kind:       "ReplicationController",
				Name:       rcName,
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
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(70),
						},
					},
				},
			},
		},
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create multi-metric HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 5,
		DesiredReplicas: 7,
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
						AverageUtilization: int32Ptr(150),
					},
				},
			},
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceMemory,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: int32Ptr(75),
					},
				},
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update multi-metric HPA %s status: %v", hpaName, err)
	}
}

// writeTempConfigFile writes content to a temporary file and registers cleanup
// to remove it when the test finishes. Returns the file path.
func writeTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "hpa-status-config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp config file: %v", err)
	}
	path := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		t.Fatalf("failed to write temp config file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp config file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}

// parseHealthScore extracts the healthScore from JSON output for comparison.
func parseHealthScore(t *testing.T, raw string) int {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}
	analysis, ok := result["analysis"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON output missing top-level 'analysis' object")
	}
	score, ok := analysis["healthScore"].(float64)
	if !ok {
		t.Fatalf("JSON analysis 'healthScore' is not numeric, got: %T", analysis["healthScore"])
	}
	return int(score)
}

// firstN returns the first n characters of s, or the full string if shorter.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
