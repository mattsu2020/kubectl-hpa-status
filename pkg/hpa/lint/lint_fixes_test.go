package lint

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateAutoFix_UnknownRule(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	fix := generateAutoFix("unknown-rule", hpa)
	if fix != nil {
		t.Error("expected nil for unknown rule")
	}
}

func TestFixMissingScaleDownBehavior(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "app",
			},
		},
	}

	fix := generateAutoFix("behavior-scaledown", hpa)
	if fix == nil {
		t.Fatal("expected non-nil fix for behavior-scaledown")
	}
	if fix.Patch == "" {
		t.Error("expected non-empty patch")
	}
	if fix.Command == "" {
		t.Error("expected non-empty command")
	}
	if fix.Before != "No scaleDown behavior configured" {
		t.Errorf("unexpected Before: %s", fix.Before)
	}
	if fix.After != "scaleDown with 300s stabilization + 50%/60s policy" {
		t.Errorf("unexpected After: %s", fix.After)
	}
	if fix.Risk == "" {
		t.Error("expected non-empty Risk")
	}
}

func TestFixHighUtilizationTarget(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "app",
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

	fix := generateAutoFix("target-utilization", hpa)
	if fix == nil {
		t.Fatal("expected non-nil fix for target-utilization")
	}
	if fix.Before != "95%" {
		t.Errorf("expected Before=95%%, got %s", fix.Before)
	}
	if fix.After != "80%" {
		t.Errorf("expected After=80%%, got %s", fix.After)
	}
	if fix.Risk == "" {
		t.Error("expected non-empty Risk")
	}
}

func TestFixHighUtilizationTarget_NoUtilization(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
		},
	}

	fix := generateAutoFix("target-utilization", hpa)
	if fix != nil {
		t.Error("expected nil fix when no utilization target set")
	}
}

func TestFixTightTolerance_ScaleUp(t *testing.T) {
	tol := resource.MustParse("0.005")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					Tolerance: &tol,
				},
			},
		},
	}

	fix := generateAutoFix("tolerance", hpa)
	if fix == nil {
		t.Fatal("expected non-nil fix for tolerance")
	}
	if fix.Patch == "" {
		t.Error("expected non-empty patch")
	}
	if fix.Command == "" {
		t.Error("expected non-empty command")
	}
	if fix.Risk == "" {
		t.Error("expected non-empty Risk")
	}
}

func TestFixTightTolerance_ScaleDown(t *testing.T) {
	tol := resource.MustParse("0.003")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					Tolerance: &tol,
				},
			},
		},
	}

	fix := generateAutoFix("tolerance", hpa)
	if fix == nil {
		t.Fatal("expected non-nil fix for tolerance (scaleDown)")
	}
}

func TestFixTightTolerance_NoTolerance(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{},
			},
		},
	}

	fix := generateAutoFix("tolerance", hpa)
	if fix != nil {
		t.Error("expected nil when no tolerance set")
	}
}

func TestFixLongStabilizationWindow(t *testing.T) {
	window := int32(1800)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: &window,
				},
			},
		},
	}

	fix := generateAutoFix("stabilization-window", hpa)
	if fix == nil {
		t.Fatal("expected non-nil fix for stabilization-window")
	}
	if fix.Before != "1800s" {
		t.Errorf("expected Before=1800s, got %s", fix.Before)
	}
	if fix.After != "300s (5m)" {
		t.Errorf("expected After=300s (5m), got %s", fix.After)
	}
	if fix.Risk == "" {
		t.Error("expected non-empty Risk")
	}
}

func TestFixLongStabilizationWindow_NoBehavior(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "my-hpa", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
		},
	}

	fix := generateAutoFix("stabilization-window", hpa)
	if fix != nil {
		t.Error("expected nil when no behavior configured")
	}
}
