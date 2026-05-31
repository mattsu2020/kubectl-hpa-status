//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
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
		// The command may return a non-zero exit code due to ERROR health, but
		// Execute() returns nil because the exit code is handled separately.
		t.Fatalf("failed to execute status broken-hpa --explain: %v. Output:\n%s", err, buf.String())
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
