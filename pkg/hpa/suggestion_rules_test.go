package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScalingActiveRule(t *testing.T) {
	tests := []struct {
		name    string
		hpa     *autoscalingv2.HorizontalPodAutoscaler
		wantNil bool
		wantLen int
	}{
		{
			name: "ScalingActive not True returns suggestions",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "my-metric"},
							},
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingActive",
							Status: corev1.ConditionFalse,
							Reason: "FailedGetExternalMetric",
						},
					},
				},
			},
			wantNil: false,
			wantLen: 2, // Restore metric availability + external metric freshness
		},
		{
			name: "ScalingActive True returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingActive",
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			wantNil: true,
		},
		{
			name:    "No conditions returns nil",
			hpa:     &autoscalingv2.HorizontalPodAutoscaler{},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scalingActiveRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestions, got nil")
			}
			if !tt.wantNil && len(got) != tt.wantLen {
				t.Fatalf("expected %d suggestions, got %d", tt.wantLen, len(got))
			}
		})
	}
}

func TestScalingLimitedMaxRule(t *testing.T) {
	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		wantNil   bool
		wantTitle string
	}{
		{
			name: "Capped at max with current replicas > 0 suggests raising max",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
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
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 10,
					CurrentMetrics: []autoscalingv2.MetricStatus{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceCPU,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(100),
								},
							},
						},
					},
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingActive",
							Status: corev1.ConditionTrue,
						},
						{
							Type:   "ScalingLimited",
							Status: corev1.ConditionTrue,
							Reason: "DesiredReplicasAboveMaxReplicas",
						},
					},
				},
			},
			wantNil:   false,
			wantTitle: "Raise maxReplicas",
		},
		{
			name: "Capped at max with current replicas 0 does not suggest",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MaxReplicas: 10,
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 0,
					DesiredReplicas: 10,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingActive",
							Status: corev1.ConditionTrue,
						},
						{
							Type:   "ScalingLimited",
							Status: corev1.ConditionTrue,
							Reason: "DesiredReplicasAboveMaxReplicas",
						},
					},
				},
			},
			wantNil: true,
		},
		{
			name: "Not capped at max returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MaxReplicas: 10,
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					DesiredReplicas: 5,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingLimited",
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scalingLimitedMaxRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestion, got nil")
			}
			if !tt.wantNil && len(got) != 1 {
				t.Fatalf("expected 1 suggestion, got %d", len(got))
			}
			if !tt.wantNil && got[0].Title != tt.wantTitle {
				t.Fatalf("expected title %q, got %q", tt.wantTitle, got[0].Title)
			}
		})
	}
}

func TestScalingLimitedMinRule(t *testing.T) {
	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		min       int32
		wantNil   bool
		wantTitle string
	}{
		{
			name: "Capped at min with min > 1 suggests lowering min",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(3),
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					DesiredReplicas: 3,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingActive",
							Status: corev1.ConditionTrue,
						},
						{
							Type:   "ScalingLimited",
							Status: corev1.ConditionTrue,
							Reason: "DesiredReplicasBelowMinReplicas",
						},
					},
				},
			},
			min:       3,
			wantNil:   false,
			wantTitle: "Lower minReplicas",
		},
		{
			name: "Capped at min with min = 1 does not suggest",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(1),
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					DesiredReplicas: 1,
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "ScalingLimited",
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			min:     1,
			wantNil: true,
		},
		{
			name: "Not capped at min returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(2),
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					DesiredReplicas: 5,
				},
			},
			min:     2,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scalingLimitedMinRule(tt.hpa, tt.min)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestion, got nil")
			}
			if !tt.wantNil && got[0].Title != tt.wantTitle {
				t.Fatalf("expected title %q, got %q", tt.wantTitle, got[0].Title)
			}
		})
	}
}

func TestScaleDownStabilizedRule(t *testing.T) {
	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		wantNil   bool
		wantTitle string
	}{
		{
			name: "ScaleDownStabilized with high window suggests shortening",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: int32Ptr(600),
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "AbleToScale",
							Status: corev1.ConditionTrue,
							Reason: "ScaleDownStabilized",
						},
					},
				},
			},
			wantNil:   false,
			wantTitle: "Shorten scale-down stabilization",
		},
		{
			name: "ScaleDownStabilized with default 300s does not suggest",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: int32Ptr(300),
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "AbleToScale",
							Reason: "ScaleDownStabilized",
						},
					},
				},
			},
			wantNil: true,
		},
		{
			name: "Not ScaleDownStabilized returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "AbleToScale",
							Reason: "SucceededRescale",
						},
					},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaleDownStabilizedRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestion, got nil")
			}
			if !tt.wantNil && got[0].Title != tt.wantTitle {
				t.Fatalf("expected title %q, got %q", tt.wantTitle, got[0].Title)
			}
		})
	}
}

func TestBehaviorPolicyRule(t *testing.T) {
	tests := []struct {
		name       string
		hpa        *autoscalingv2.HorizontalPodAutoscaler
		wantNil    bool
		wantTitles []string
	}{
		{
			name: "Scale-up pressure with missing policies suggests adding them",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
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
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 2,
					DesiredReplicas: 2,
					CurrentMetrics: []autoscalingv2.MetricStatus{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceCPU,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(100),
								},
							},
						},
					},
				},
			},
			wantNil:    false,
			wantTitles: []string{"Add explicit scale-up policy"},
		},
		{
			name: "Scale-down pressure with missing policies suggests adding them",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
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
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 2,
					DesiredReplicas: 2,
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
				},
			},
			wantNil:    false,
			wantTitles: []string{"Add explicit scale-down policy"},
		},
		{
			name: "No pressure returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 2,
					DesiredReplicas: 2,
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := behaviorPolicyRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestions, got nil")
			}
			if !tt.wantNil {
				if len(got) != len(tt.wantTitles) {
					t.Fatalf("expected %d suggestions, got %d", len(tt.wantTitles), len(got))
				}
				for i, title := range tt.wantTitles {
					if got[i].Title != title {
						t.Fatalf("expected title %q, got %q", title, got[i].Title)
					}
				}
			}
		})
	}
}

func TestToleranceRule(t *testing.T) {
	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		wantNil   bool
		wantTitle string
	}{
		{
			name: "Metric slightly above target with stable replicas suggests tolerance",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
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
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 5,
					CurrentMetrics: []autoscalingv2.MetricStatus{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceCPU,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(85), // 1.0625x target
								},
							},
						},
					},
				},
			},
			wantNil:   false,
			wantTitle: "Review scale-up tolerance",
		},
		{
			name: "Replicas changing returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 7,
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toleranceRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestion, got nil")
			}
			if !tt.wantNil && got[0].Title != tt.wantTitle {
				t.Fatalf("expected title %q, got %q", tt.wantTitle, got[0].Title)
			}
		})
	}
}

func TestMetricMixRule(t *testing.T) {
	tests := []struct {
		name       string
		hpa        *autoscalingv2.HorizontalPodAutoscaler
		wantNil    bool
		wantTitles []string
	}{
		{
			name: "Only external metrics suggests resource safety metric",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "my-metric"},
							},
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscalingv2.MetricStatus{
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricStatus{
								Metric: autoscalingv2.MetricIdentifier{Name: "my-metric"},
							},
						},
					},
				},
			},
			wantNil:    false,
			wantTitles: []string{"Consider a resource safety metric"},
		},
		{
			name: "Has resource metrics does not suggest safety metric",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: corev1.ResourceCPU,
							},
						},
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "my-metric"},
							},
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentMetrics: []autoscalingv2.MetricStatus{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceCPU,
							},
						},
					},
				},
			},
			wantNil:    false,
			wantTitles: []string{"Investigate stale external metric"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := metricMixRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestions, got nil")
			}
			if !tt.wantNil {
				found := false
				for _, suggestion := range got {
					for _, wantTitle := range tt.wantTitles {
						if suggestion.Title == wantTitle {
							found = true
							break
						}
					}
				}
				if !found {
					t.Fatalf("expected to find one of %v in suggestions, got %v", tt.wantTitles, got)
				}
			}
		})
	}
}

func TestKEDARule(t *testing.T) {
	tests := []struct {
		name      string
		hpa       *autoscalingv2.HorizontalPodAutoscaler
		wantNil   bool
		wantTitle string
	}{
		{
			name: "KEDA-managed HPA returns suggestion",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "keda-hpa-worker",
					Labels: map[string]string{
						"scaledobject.keda.sh/name": "worker",
					},
				},
			},
			wantNil:   false,
			wantTitle: "Inspect KEDA ScaledObject",
		},
		{
			name: "Non-KEDA HPA returns nil",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "regular-hpa",
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kedaRule(tt.hpa, 1)
			if tt.wantNil && got != nil {
				t.Fatalf("expected nil, got %d suggestions", len(got))
			}
			if !tt.wantNil && got == nil {
				t.Fatal("expected suggestion, got nil")
			}
			if !tt.wantNil && got[0].Title != tt.wantTitle {
				t.Fatalf("expected title %q, got %q", tt.wantTitle, got[0].Title)
			}
		})
	}
}

func TestNoSafeFixRule(t *testing.T) {
	got := noSafeFixSuggestion()
	if got.Title != "No safe automatic fix" {
		t.Fatalf("expected title 'No safe automatic fix', got %q", got.Title)
	}
}

func TestCoreSuggestionRules(t *testing.T) {
	rules := coreSuggestionRules()
	if len(rules) != 8 {
		t.Fatalf("expected 8 rules, got %d", len(rules))
	}
}

// Helper functions

func int32Ptr(i int32) *int32 {
	return &i
}

func resourcePtr(q resource.Quantity) *resource.Quantity {
	return &q
}
