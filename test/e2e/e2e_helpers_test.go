//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/json"
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
)

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

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "hpa-status-e2e-",
		},
	}
	created, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}
	nsName := created.Name
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

// createScaleToZeroHPA creates an HPA with minReplicas=0, CPU metric, and
// status indicating zero current and desired replicas (scale-to-zero state).
// Returns false if the API server rejects minReplicas=0 (feature gate not enabled).
func createScaleToZeroHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) bool {
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
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{
							Name: "queue-depth",
						},
						Target: autoscalingv2.MetricTarget{
							Type:  autoscalingv2.AverageValueMetricType,
							Value: resource.NewQuantity(10, resource.DecimalSI),
						},
					},
				},
			},
		},
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "minReplicas") || strings.Contains(err.Error(), "Invalid value") {
			return false
		}
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
	return true
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

// createBehaviorPolicyHPA creates an HPA with explicit scaleUp/scaleDown
// policies (percent and pods) so the behavior subcommand and status
// --explain path can assert policy visualization for ROADMAP coverage.
func createBehaviorPolicyHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(2)
	scaleUpStab := int32(0)
	scaleDownStab := int32(180)
	selectMax := autoscalingv2.MaxChangePolicySelect
	selectMin := autoscalingv2.MinChangePolicySelect
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
			MaxReplicas: 20,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(70),
						},
					},
				},
			},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleUpStab,
					SelectPolicy:               &selectMax,
					Policies: []autoscalingv2.HPAScalingPolicy{
						{Type: autoscalingv2.PercentScalingPolicy, Value: 100, PeriodSeconds: 15},
						{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 15},
					},
				},
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &scaleDownStab,
					SelectPolicy:               &selectMin,
					Policies: []autoscalingv2.HPAScalingPolicy{
						{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 60},
					},
				},
			},
		},
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create behavior-policy HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 4,
		DesiredReplicas: 8,
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
						AverageUtilization: int32Ptr(140),
					},
				},
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update behavior-policy HPA %s status: %v", hpaName, err)
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
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw:\n%s", err, raw)
	}
	analysis, ok := result["analysis"].(map[string]any)
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

// createBrokenExternalMetricHPA creates an HPA whose ScalingActive condition is
// False with reason FailedGetResourceMetric while the spec references an
// External metric source. This simulates an external metrics adapter failure.
func createBrokenExternalMetricHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
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
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricSource{
						Metric: autoscalingv2.MetricIdentifier{
							Name: "queue-depth",
						},
						Target: autoscalingv2.MetricTarget{
							Type:         autoscalingv2.AverageValueMetricType,
							AverageValue: resourcePtr("5"),
						},
					},
				},
			},
		},
	}
	hpa, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).Create(ctx, hpa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create broken external-metric HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: 2,
		DesiredReplicas: 2,
		Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
			{
				Type:               autoscalingv2.ScalingActive,
				Status:             corev1.ConditionFalse,
				Reason:             "FailedGetResourceMetric",
				Message:            "unable to get metrics for resource and external metric queue-depth: no metrics serving queue-depth",
				LastTransitionTime: metav1.Now(),
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update broken external-metric HPA %s status: %v", hpaName, err)
	}
}

// createTooFewReplicasHPA creates an HPA sitting at minReplicas with
// desired==current==minReplicas and ScalingActive=True. This exercises the
// "at minReplicas" summary path without triggering ScalingLimited penalties.
func createTooFewReplicasHPA(t *testing.T, client *kubernetes.Clientset, nsName, hpaName, rcName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	minReplicas := int32(3)
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
		t.Fatalf("failed to create too-few-replicas HPA %s: %v", hpaName, err)
	}

	hpa.Status = autoscalingv2.HorizontalPodAutoscalerStatus{
		CurrentReplicas: minReplicas,
		DesiredReplicas: minReplicas,
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
		},
		CurrentMetrics: []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: int32Ptr(20),
					},
				},
			},
		},
	}
	if _, err := client.AutoscalingV2().HorizontalPodAutoscalers(nsName).UpdateStatus(ctx, hpa, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update too-few-replicas HPA %s status: %v", hpaName, err)
	}
}
