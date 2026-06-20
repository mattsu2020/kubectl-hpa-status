package policy

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func int32PtrForPolicyTest(v int32) *int32 { return &v }

func intPtr(v int) *int { return &v }

func TestEvaluatePolicies_PolicySetShortForm(t *testing.T) {
	policy := File{
		Policies: []Set{
			{
				Name:     "production-stabilization",
				Selector: map[string]string{"environment": "production"},
				Rules: []Rule{
					{Type: "stabilizationWindowSeconds", Min: intPtr(300), Severity: "warning"},
					{Type: "maxReplicas", MaxMultiplierFromCurrent: intPtr(5), Severity: "critical"},
				},
			},
		},
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
			Labels:    map[string]string{"environment": "production"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 30,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{StabilizationWindowSeconds: int32PtrForPolicyTest(60)},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 4},
	}

	report := EvaluatePolicies(hpa, policy)
	if len(report.Violations) != 2 {
		t.Fatalf("expected 2 violations, got %#v", report.Violations)
	}
	if report.Violations[0].RuleID != "stabilization-window" {
		t.Fatalf("expected stabilization rule, got %#v", report.Violations[0])
	}
	if report.Violations[1].RuleID != "max-replicas-from-current" {
		t.Fatalf("expected max replicas from current rule, got %#v", report.Violations[1])
	}
}
