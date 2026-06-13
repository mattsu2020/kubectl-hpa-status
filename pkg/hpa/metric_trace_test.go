package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildMetricDecisionTrace(t *testing.T) {
	tests := []struct {
		name               string
		hpa                *autoscalingv2.HorizontalPodAutoscaler
		minReplicas        int32
		wantNil            bool
		wantMetricCount    int
		wantWinner         string
		wantConfidence     Confidence
		wantWithinTolIdx   int // index of entry expected to be within tolerance, -1 means skip check
		wantSuppressedDown bool
		wantExternalName   string
	}{
		{
			name:             "Nil HPA returns nil",
			hpa:              nil,
			minReplicas:      1,
			wantNil:          true,
			wantWithinTolIdx: -1,
		},
		{
			name: "Single resource metric produces trace but only one entry",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
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
					DesiredReplicas: 5,
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
			minReplicas:      1,
			wantNil:          false,
			wantMetricCount:  1,
			wantWinner:       "cpu",
			wantConfidence:   ConfidenceMedium,
			wantWithinTolIdx: -1,
		},
		{
			name: "Two resource metrics where CPU has higher impact",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
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
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: corev1.ResourceMemory,
								Target: autoscalingv2.MetricTarget{
									Type:               autoscalingv2.UtilizationMetricType,
									AverageUtilization: int32Ptr(80),
								},
							},
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 4,
					DesiredReplicas: 6,
					CurrentMetrics: []autoscalingv2.MetricStatus{
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceCPU,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(120), // 1.5x target
								},
							},
						},
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceMemory,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(88), // 1.1x target
								},
							},
						},
					},
				},
			},
			minReplicas:      1,
			wantNil:          false,
			wantMetricCount:  2,
			wantWinner:       "cpu",
			wantConfidence:   ConfidenceMedium,
			wantWithinTolIdx: -1,
		},
		{
			name: "desiredReplicas == maxReplicas gives low confidence winner",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
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
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: corev1.ResourceMemory,
								Target: autoscalingv2.MetricTarget{
									Type:               autoscalingv2.UtilizationMetricType,
									AverageUtilization: int32Ptr(80),
								},
							},
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 10,
					DesiredReplicas: 10, // == maxReplicas
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
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceMemory,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(90),
								},
							},
						},
					},
				},
			},
			minReplicas:      1,
			wantNil:          false,
			wantMetricCount:  2,
			wantWinner:       "cpu",
			wantConfidence:   ConfidenceLow,
			wantWithinTolIdx: -1,
		},
		{
			name: "Metric within tolerance",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
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
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: corev1.ResourceMemory,
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
									AverageUtilization: int32Ptr(120), // 1.5x - not within tolerance
								},
							},
						},
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceMemory,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(82), // 1.025x - within tolerance
								},
							},
						},
					},
				},
			},
			minReplicas:      1,
			wantNil:          false,
			wantMetricCount:  2,
			wantWinner:       "cpu",
			wantConfidence:   ConfidenceMedium,
			wantWithinTolIdx: 1,
		},
		{
			name: "Stabilization window active",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MaxReplicas: 10,
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: int32Ptr(300),
						},
					},
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
									AverageUtilization: int32Ptr(120),
								},
							},
						},
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceMemory,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(60),
								},
							},
						},
					},
					Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
						{
							Type:   "AbleToScale",
							Status: corev1.ConditionTrue,
							Reason: "ScaleDownStabilized",
						},
					},
				},
			},
			minReplicas:        1,
			wantNil:            false,
			wantMetricCount:    2,
			wantWinner:         "cpu",
			wantConfidence:     ConfidenceMedium,
			wantSuppressedDown: true,
			wantWithinTolIdx:   -1,
		},
		{
			name: "External metric alongside resource metrics",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa"},
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
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "http_requests"},
								Target: autoscalingv2.MetricTarget{
									Type:         autoscalingv2.AverageValueMetricType,
									AverageValue: resourcePtr(resource.MustParse("500")),
								},
							},
						},
					},
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 4,
					DesiredReplicas: 6,
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
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricStatus{
								Metric: autoscalingv2.MetricIdentifier{Name: "http_requests"},
								Current: autoscalingv2.MetricValueStatus{
									AverageValue: resourcePtr(resource.MustParse("800")),
								},
							},
						},
					},
				},
			},
			minReplicas:      1,
			wantNil:          false,
			wantMetricCount:  2,
			wantExternalName: "http_requests",
			wantWithinTolIdx: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMetricDecisionTrace(tt.hpa, tt.minReplicas)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil MetricDecisionTrace, got nil")
			}

			if len(got.Metrics) != tt.wantMetricCount {
				t.Fatalf("expected %d metrics, got %d", tt.wantMetricCount, len(got.Metrics))
			}

			if tt.wantWinner != "" && got.Winner != tt.wantWinner {
				t.Errorf("expected winner %q, got %q", tt.wantWinner, got.Winner)
			}

			if tt.wantConfidence != "" && got.WinnerConfidence != tt.wantConfidence {
				t.Errorf("expected winner confidence %q, got %q", tt.wantConfidence, got.WinnerConfidence)
			}

			if tt.wantWithinTolIdx >= 0 && tt.wantWithinTolIdx < len(got.Metrics) {
				if !got.Metrics[tt.wantWithinTolIdx].WithinTolerance {
					t.Errorf("expected metric %d (%s) to be within tolerance", tt.wantWithinTolIdx, got.Metrics[tt.wantWithinTolIdx].Name)
				}
			}

			if tt.wantSuppressedDown {
				if got.StabilizationEffect == nil {
					t.Fatal("expected stabilization effect, got nil")
				}
				if !got.StabilizationEffect.SuppressedScaleDown {
					t.Error("expected suppressed scale-down to be true")
				}
			}

			if tt.wantExternalName != "" {
				found := false
				for _, m := range got.Metrics {
					if m.Name == tt.wantExternalName && m.Type == "External" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected to find external metric %q, got %v", tt.wantExternalName, got.Metrics)
				}
			}

			if got.Summary == "" {
				t.Error("expected non-empty summary")
			}
		})
	}
}

func TestDetectMetricDecisionTrace(t *testing.T) {
	tests := []struct {
		name        string
		hpa         *autoscalingv2.HorizontalPodAutoscaler
		minReplicas int32
		wantTrace   bool
	}{
		{
			name: "Single metric does not produce trace",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
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
					DesiredReplicas: 5,
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
			minReplicas: 1,
			wantTrace:   false,
		},
		{
			name: "Two metrics produces trace",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
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
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricSource{
								Name: corev1.ResourceMemory,
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
									AverageUtilization: int32Ptr(100),
								},
							},
						},
						{
							Type: autoscalingv2.ResourceMetricSourceType,
							Resource: &autoscalingv2.ResourceMetricStatus{
								Name: corev1.ResourceMemory,
								Current: autoscalingv2.MetricValueStatus{
									AverageUtilization: int32Ptr(90),
								},
							},
						},
					},
				},
			},
			minReplicas: 1,
			wantTrace:   true,
		},
		{
			name: "No current metrics does not produce trace",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MaxReplicas: 10,
				},
				Status: autoscalingv2.HorizontalPodAutoscalerStatus{
					CurrentReplicas: 5,
					DesiredReplicas: 5,
				},
			},
			minReplicas: 1,
			wantTrace:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Analysis{}
			result := detectMetricDecisionTrace(a, tt.hpa, tt.minReplicas)

			if tt.wantTrace && result.MetricDecisionTrace == nil {
				t.Fatal("expected MetricDecisionTrace to be set, got nil")
			}
			if !tt.wantTrace && result.MetricDecisionTrace != nil {
				t.Fatalf("expected no MetricDecisionTrace, got %+v", result.MetricDecisionTrace)
			}
		})
	}
}
