//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
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

func int32Ptr(v int32) *int32 {
	return &v
}
