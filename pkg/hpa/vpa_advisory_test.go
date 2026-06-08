package hpa

import (
	"encoding/json"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildVPAAdvisorHPA(withResourceMetric bool) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    int32Ptr(1),
			MaxReplicas:    10,
		},
	}
	if withResourceMetric {
		hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					AverageUtilization: int32Ptr(80),
				},
			},
		}}
	}
	return hpa
}

func buildExternalOnlyHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    int32Ptr(1),
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
					Target: autoscalingv2.MetricTarget{Type: autoscalingv2.ValueMetricType},
				},
			}},
		},
	}
}

func TestAnalyzeVPAAdvisory(t *testing.T) {
	tests := []struct {
		name                string
		hpa                 *autoscalingv2.HorizontalPodAutoscaler
		vpa                 *VPAConflictInfo
		wantNil             bool
		wantLevel           VPAConflictLevel
		wantSafe            bool
		wantConflictRes     []string
		wantVPAPatchContain string
	}{
		{
			name:    "nil HPA returns nil",
			hpa:     nil,
			vpa:     &VPAConflictInfo{VPAName: "v", UpdateMode: "Auto"},
			wantNil: true,
		},
		{
			name:    "nil VPA returns nil",
			hpa:     buildVPAAdvisorHPA(true),
			vpa:     nil,
			wantNil: true,
		},
		{
			name:      "VPA Off mode - no conflict",
			hpa:       buildVPAAdvisorHPA(true),
			vpa:       &VPAConflictInfo{VPAName: "web-vpa", TargetKind: "Deployment", TargetName: "web", UpdateMode: "Off"},
			wantLevel: VPAConflictNone,
			wantSafe:  true,
		},
		{
			name:      "VPA Recommender mode - no conflict",
			hpa:       buildVPAAdvisorHPA(true),
			vpa:       &VPAConflictInfo{VPAName: "web-vpa", TargetKind: "Deployment", TargetName: "web", UpdateMode: "Recommender"},
			wantLevel: VPAConflictNone,
			wantSafe:  true,
		},
		{
			name:      "VPA Initial mode with resource overlap - warning",
			hpa:       buildVPAAdvisorHPA(true),
			vpa:       &VPAConflictInfo{VPAName: "web-vpa", TargetKind: "Deployment", TargetName: "web", UpdateMode: "Initial", ControlledResources: []string{"cpu"}},
			wantLevel: VPAConflictWarning,
			wantSafe:  true,
		},
		{
			name:                "VPA Auto mode with resource overlap - error",
			hpa:                 buildVPAAdvisorHPA(true),
			vpa:                 &VPAConflictInfo{VPAName: "web-vpa", TargetKind: "Deployment", TargetName: "web", UpdateMode: "Auto", ControlledResources: []string{"cpu", "memory"}},
			wantLevel:           VPAConflictError,
			wantSafe:            false,
			wantVPAPatchContain: "Initial",
		},
		{
			name:      "HPA with only external metrics - no conflict",
			hpa:       buildExternalOnlyHPA(),
			vpa:       &VPAConflictInfo{VPAName: "web-vpa", TargetKind: "Deployment", TargetName: "web", UpdateMode: "Auto"},
			wantLevel: VPAConflictNone,
			wantSafe:  true,
		},
		{
			name:            "conflict resources correctly identified",
			hpa:             buildVPAAdvisorHPA(true),
			vpa:             &VPAConflictInfo{VPAName: "web-vpa", TargetKind: "Deployment", TargetName: "web", UpdateMode: "Auto", ControlledResources: []string{"cpu", "memory"}},
			wantLevel:       VPAConflictError,
			wantConflictRes: []string{"cpu"},
			wantSafe:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AnalyzeVPAAdvisory(tc.hpa, tc.vpa)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil advisory, got nil")
			}

			if got.Level != tc.wantLevel {
				t.Errorf("Level = %q, want %q", got.Level, tc.wantLevel)
			}
			if got.SafeCoexistence != tc.wantSafe {
				t.Errorf("SafeCoexistence = %v, want %v", got.SafeCoexistence, tc.wantSafe)
			}

			if tc.wantConflictRes != nil {
				if len(got.ConflictResources) != len(tc.wantConflictRes) {
					t.Fatalf("ConflictResources = %v, want %v", got.ConflictResources, tc.wantConflictRes)
				}
				for i, r := range tc.wantConflictRes {
					if got.ConflictResources[i] != r {
						t.Errorf("ConflictResources[%d] = %q, want %q", i, got.ConflictResources[i], r)
					}
				}
			}

			if tc.wantVPAPatchContain != "" {
				if !json.Valid([]byte(got.VPAPatch)) {
					t.Errorf("VPAPatch is not valid JSON: %q", got.VPAPatch)
				}
				if got.VPAPatch == "" {
					t.Error("VPAPatch is empty, expected non-empty")
				}
			}

			if tc.wantLevel == VPAConflictWarning || tc.wantLevel == VPAConflictError {
				if len(got.Recommendations) == 0 {
					t.Error("Recommendations is empty for non-NONE level")
				}
				if got.Explanation == "" {
					t.Error("Explanation is empty for non-NONE level")
				}
			}
		})
	}
}
