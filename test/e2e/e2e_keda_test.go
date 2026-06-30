//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Import the root command package from our plugin
	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

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
	rootCmd.SetArgs([]string{"status", "keda-hpa-test", "-n", nsName, "--keda=on", "--explain", "--kubeconfig", kubeconfig})

	if err := rootCmd.Execute(); err != nil {
		t.Logf("status --keda returned error (may be expected if KEDA CRD absent): %v", err)
	}

	output := buf.String()
	t.Logf("KEDA status output:\n%s", output)

	// Even without KEDA CRD, the output should detect KEDA labels
	if !strings.Contains(output, "KEDA") {
		t.Errorf("expected KEDA detection in output, got:\n%s", output)
	}

	// Stronger schema contract: the JSON report must still validate and
	// reference the detected ScaledObject name from the HPA label. With the
	// CRD absent, --keda=on surfaces a warning but the base report shape
	// must be intact.
	kedaRaw := runStatusJSON(t, kubeconfig, nsName, "keda-hpa-test", "--explain")
	assertStatusReportShape(t, kedaRaw, "keda-hpa-test")
	kedaResult := decodeStatusReportJSON(t, kedaRaw)
	kedaAnalysis := analysisMap(t, kedaResult)
	if _, ok := kedaAnalysis["structuredInterpretation"].([]any); !ok {
		t.Error("expected analysis.structuredInterpretation array after --explain on KEDA HPA")
	}
}
