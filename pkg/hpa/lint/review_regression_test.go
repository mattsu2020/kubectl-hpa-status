package lint

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestLintUtilizationFixPreservesMetricsAndTargetsFinding(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Metrics: []autoscalingv2.MetricSpec{
		{Type: autoscalingv2.ExternalMetricSourceType, External: &autoscalingv2.ExternalMetricSource{Metric: autoscalingv2.MetricIdentifier{Name: "queue"}, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: ptr.To(resource.MustParse("10"))}}},
		{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceMemory, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: ptr.To(int32(95))}}},
		{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: ptr.To(int32(97))}}},
	}}}
	original := hpa.DeepCopy()
	findings := lintHighUtilizationTarget(hpa)
	if len(findings) != 2 {
		t.Fatalf("findings: %+v", findings)
	}
	for i, finding := range findings {
		if finding.AutoFix == nil {
			t.Fatal("missing fix")
		}
		var patch autoscalingv2.HorizontalPodAutoscaler
		if err := json.Unmarshal([]byte(finding.AutoFix.Patch), &patch); err != nil {
			t.Fatal(err)
		}
		want := original.DeepCopy().Spec.Metrics
		want[i+1].Resource.Target.AverageUtilization = ptr.To(int32(80))
		if !reflect.DeepEqual(patch.Spec.Metrics, want) {
			t.Fatalf("finding %d altered unrelated metrics: %+v", i, patch.Spec.Metrics)
		}
	}
	if !reflect.DeepEqual(hpa, original) {
		t.Fatal("lint mutated input")
	}
}

func TestLintToleranceFixTargetsEachDirection(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleUp:   &autoscalingv2.HPAScalingRules{Tolerance: ptr.To(resource.MustParse("0.2"))},
		ScaleDown: &autoscalingv2.HPAScalingRules{Tolerance: ptr.To(resource.MustParse("0.005"))},
	}}}
	findings := lintTolerance(hpa)
	if len(findings) != 1 || findings[0].AutoFix == nil {
		t.Fatalf("findings: %+v", findings)
	}
	if !strings.Contains(findings[0].AutoFix.Patch, "scaleDown") || strings.Contains(findings[0].AutoFix.Patch, "scaleUp") {
		t.Fatalf("wrong direction: %s", findings[0].AutoFix.Patch)
	}
	hpa.Spec.Behavior.ScaleUp.Tolerance = ptr.To(resource.MustParse("0.005"))
	findings = lintTolerance(hpa)
	if len(findings) != 2 {
		t.Fatalf("findings: %+v", findings)
	}
	for i, direction := range []string{"scaleUp", "scaleDown"} {
		var patch struct {
			Spec struct{ Behavior map[string]json.RawMessage }
		}
		if err := json.Unmarshal([]byte(findings[i].AutoFix.Patch), &patch); err != nil {
			t.Fatal(err)
		}
		if len(patch.Spec.Behavior) != 1 || patch.Spec.Behavior[direction] == nil {
			t.Fatalf("wrong direction: %s", findings[i].AutoFix.Patch)
		}
	}
}

func TestAutoFixRejectsUnavailableTargets(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{Metrics: []autoscalingv2.MetricSpec{
		{Type: autoscalingv2.ExternalMetricSourceType},
		{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU}},
	}}}
	original := hpa.DeepCopy()
	for _, index := range []int{-1, 0, 1, 2} {
		if fix := fixUtilizationTargetAt(hpa, index); fix != nil {
			t.Fatalf("invalid metric %d produced a patch: %+v", index, fix)
		}
	}
	if fix := fixToleranceDirection(hpa, "scaleDown"); fix != nil {
		t.Fatalf("absent behavior produced patch: %+v", fix)
	}
	hpa.Spec.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleUp: &autoscalingv2.HPAScalingRules{}}
	for _, direction := range []string{"scaleUp", "scaleDown", "invalid"} {
		if fix := fixToleranceDirection(hpa, direction); fix != nil {
			t.Fatalf("unavailable tolerance %s produced patch: %+v", direction, fix)
		}
	}
	hpa.Spec.Behavior = nil
	if !reflect.DeepEqual(hpa, original) {
		t.Fatal("invalid fixes mutated input")
	}
}
