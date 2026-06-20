package lint

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// containsString is a local test helper mirroring pkg/hpa.containsString.
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

func int32Ptr(v int32) *int32 { return &v }

func TestLintHPA_NilHPA(t *testing.T) {
	result := Run(nil)
	if result.Pass {
		t.Error("expected pass=false for nil HPA")
	}
	if result.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Errors)
	}
}

func TestLintHPA_ValidHPA(t *testing.T) {
	minReplicas := int32(2)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(60),
						},
					},
				},
			},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: int32Ptr(300),
					Policies: []autoscalingv2.HPAScalingPolicy{
						{
							Type:          autoscalingv2.PercentScalingPolicy,
							Value:         10,
							PeriodSeconds: 60,
						},
					},
				},
			},
		},
	}

	result := Run(hpa)
	if !result.Pass {
		t.Errorf("expected pass=true, got errors=%d", result.Errors)
	}
}

func TestLintHPA_MinGreaterThanMax(t *testing.T) {
	minReplicas := int32(20)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
		},
	}

	result := Run(hpa)
	if result.Pass {
		t.Error("expected pass=false when minReplicas > maxReplicas")
	}
	found := false
	for _, f := range result.Findings {
		if f.Rule == "replica-range" && f.Severity == Error {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ERROR finding for minReplicas > maxReplicas")
	}
}

func TestLintHPA_MaxReplicasZero(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 0,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
		},
	}

	result := Run(hpa)
	if result.Pass {
		t.Error("expected pass=false when maxReplicas=0")
	}
}

func TestLintHPA_NoScaleDownBehavior(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
		},
	}

	result := Run(hpa)
	found := false
	for _, f := range result.Findings {
		if f.Rule == "behavior-scaledown" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning for missing scaleDown behavior")
	}
}

func TestLintHPA_HighUtilizationTarget(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(95),
						},
					},
				},
			},
		},
	}

	result := Run(hpa)
	found := false
	for _, f := range result.Findings {
		if f.Rule == "target-utilization" && f.Severity == Warning {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning for high utilization target")
	}
}

func TestLintHPA_SingleMetric(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: int32Ptr(60),
						},
					},
				},
			},
		},
	}

	result := Run(hpa)
	found := false
	for _, f := range result.Findings {
		if f.Rule == "metric-coverage" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected info for single metric")
	}
}

func TestLintHPA_ScaleToZero(t *testing.T) {
	minReplicas := int32(0)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
		},
	}

	result := Run(hpa)
	found := false
	for _, f := range result.Findings {
		if f.Rule == "scale-to-zero" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected info for scale-to-zero")
	}
}

func TestLintHPA_TightTolerance(t *testing.T) {
	tol := resource.MustParse("0.005")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					Tolerance: &tol,
				},
			},
		},
	}

	result := Run(hpa)
	found := false
	for _, f := range result.Findings {
		if f.Rule == "tolerance" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning for tight tolerance")
	}
}

func TestLintHPA_WideReplicaRange(t *testing.T) {
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 100,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test",
			},
		},
	}

	result := Run(hpa)
	found := false
	for _, f := range result.Findings {
		if f.Rule == "replica-range" && f.Severity == Warning {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning for wide replica range")
	}
}

func TestFormatLintSARIF(t *testing.T) {
	result := &Result{
		Findings: []Finding{
			{
				Severity: Error,
				Rule:     "replica-range",
				Message:  "minReplicas > maxReplicas",
			},
			{
				Severity: Warning,
				Rule:     "behavior-scaledown",
				Message:  "No scaleDown behavior",
			},
		},
		Errors:   1,
		Warnings: 1,
		Pass:     false,
	}

	sarif := FormatLintSARIF(result, "test.yaml")
	if sarif == "" {
		t.Error("expected non-empty SARIF output")
	}
	if !containsString(sarif, "replica-range") {
		t.Error("expected SARIF to contain rule ID")
	}
	if !containsString(sarif, "error") {
		t.Error("expected SARIF to contain error level")
	}
}
