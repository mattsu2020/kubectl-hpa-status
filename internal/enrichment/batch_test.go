package enrichment

import (
	"context"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var vpaGVRForTest = schema.GroupVersionResource{
	Group:    "autoscaling.k8s.io",
	Version:  "v1",
	Resource: "verticalpodautoscalers",
}

func kedaManagedHPA(namespace, name, targetName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{"keda.sh/scaledObjectName": name + "-so"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: targetName},
		},
	}
}

func scaledObjectUnstructured(namespace, name, targetKind, targetName string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "keda.sh/v1alpha1",
			"kind":       "ScaledObject",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"scaleTargetRef": map[string]any{
					"kind": targetKind,
					"name": targetName,
				},
			},
		},
	}
}

func vpaUnstructured(namespace, name, targetKind, targetName, updateMode string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "autoscaling.k8s.io/v1",
			"kind":       "VerticalPodAutoscaler",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"targetRef":    map[string]any{"kind": targetKind, "name": targetName},
				"updatePolicy": map[string]any{"updateMode": updateMode},
			},
		},
	}
}

func hpaWithResourceMetric(namespace, name, targetName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: targetName},
			Metrics: []autoscalingv2.MetricSpec{{
				Type:     autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU},
			}},
		},
	}
}

func TestBatchKEDA_NonManagedHPASkipped(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}: "ScaledObjectList",
		},
	)
	ec := &Context{kedaEnabled: true, dynClient: dyn}

	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "plain"}},
	}
	results, _ := BatchKEDA(context.Background(), ec, hpas)
	if len(results) != 0 {
		t.Fatalf("expected no results for non-KEDA HPA, got %v", results)
	}
}

func TestBatchKEDA_ManagedWithNoMatchingScaledObject(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}: "ScaledObjectList",
		},
	)
	ec := &Context{kedaEnabled: true, dynClient: dyn}

	hpas := []autoscalingv2.HorizontalPodAutoscaler{*kedaManagedHPA("default", "web", "web")}
	results, warnings := BatchKEDA(context.Background(), ec, hpas)
	if len(warnings) != 0 {
		t.Fatalf("expected no list-error warnings, got %v", warnings)
	}
	result, ok := results["default/web"]
	if !ok || result == nil {
		t.Fatalf("expected a result for the KEDA-managed HPA, got %v", results)
	}
	if len(result.Lines) == 0 {
		t.Fatalf("expected a diagnostic line when no ScaledObject matches, got %+v", result)
	}
}

func TestBatchKEDA_ManagedWithMatchingScaledObject(t *testing.T) {
	scheme := runtime.NewScheme()
	so := scaledObjectUnstructured("default", "web-so", "Deployment", "web")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}: "ScaledObjectList",
		},
		&so,
	)
	ec := &Context{kedaEnabled: true, dynClient: dyn}

	hpas := []autoscalingv2.HorizontalPodAutoscaler{*kedaManagedHPA("default", "web", "web")}
	results, _ := BatchKEDA(context.Background(), ec, hpas)
	result, ok := results["default/web"]
	if !ok || result == nil {
		t.Fatalf("expected a result for the matched ScaledObject, got %v", results)
	}
}

func TestBatchVPA_SkipsHPAsWithoutResourceMetrics(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{vpaGVRForTest: "VerticalPodAutoscalerList"},
	)
	ec := &Context{vpaEnabled: true, dynClient: dyn}

	hpas := []autoscalingv2.HorizontalPodAutoscaler{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "no-resource-metric"}},
	}
	results, _ := BatchVPA(context.Background(), ec, hpas)
	if len(results) != 0 {
		t.Fatalf("expected no results without resource metrics, got %v", results)
	}
}

func TestBatchVPA_MatchesActiveVPA(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{vpaGVRForTest: "VerticalPodAutoscalerList"},
		vpaUnstructured("default", "web-vpa", "Deployment", "web", "Auto"),
	)
	ec := &Context{vpaEnabled: true, dynClient: dyn}

	hpas := []autoscalingv2.HorizontalPodAutoscaler{*hpaWithResourceMetric("default", "web", "web")}
	results, warnings := BatchVPA(context.Background(), ec, hpas)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if _, ok := results["default/web"]; !ok {
		t.Fatalf("expected a conflict result for the matching active VPA, got %v", results)
	}
}

func TestBatchVPA_SkipsOffModeVPA(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{vpaGVRForTest: "VerticalPodAutoscalerList"},
		vpaUnstructured("default", "web-vpa", "Deployment", "web", "Off"),
	)
	ec := &Context{vpaEnabled: true, dynClient: dyn}

	hpas := []autoscalingv2.HorizontalPodAutoscaler{*hpaWithResourceMetric("default", "web", "web")}
	results, _ := BatchVPA(context.Background(), ec, hpas)
	if len(results) != 0 {
		t.Fatalf("expected no conflict result for Off-mode VPA, got %v", results)
	}
}
