package util

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

func TestMissingPolicies(t *testing.T) {
	rules := func() *autoscalingv2.HPAScalingRules {
		return &autoscalingv2.HPAScalingRules{Policies: []autoscalingv2.HPAScalingPolicy{{Type: autoscalingv2.PodsScalingPolicy}}}
	}

	cases := []struct {
		name     string
		behavior *autoscalingv2.HorizontalPodAutoscalerBehavior
		dir      string
		want     bool
	}{
		{"nil behavior", nil, "scaleUp", true},
		{"unknown direction", &autoscalingv2.HorizontalPodAutoscalerBehavior{}, "sideways", true},
		{"scaleUp nil rules", &autoscalingv2.HorizontalPodAutoscalerBehavior{}, "scaleUp", true},
		{
			name:     "scaleUp has policies",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleUp: rules()},
			dir:      "scaleUp",
			want:     false,
		},
		{
			name:     "scaleDown empty policies",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleDown: &autoscalingv2.HPAScalingRules{}},
			dir:      "scaleDown",
			want:     true,
		},
		{
			name:     "scaleDown has policies",
			behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{ScaleDown: rules()},
			dir:      "scaleDown",
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MissingPolicies(tc.behavior, tc.dir); got != tc.want {
				t.Fatalf("MissingPolicies = %v, want %v", got, tc.want)
			}
		})
	}
}
