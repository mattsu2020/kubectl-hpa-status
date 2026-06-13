package hpa

import (
	"bytes"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------------------------------------------------------------------------
// 1. stabilizationWindowAuditRule
// ---------------------------------------------------------------------------

func TestStabilizationWindowAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantID       string
	}{
		{
			name: "no behavior spec returns one warning finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "stabilization-window",
		},
		{
			name: "behavior with scaleDown but no stabilization window returns one warning finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							Policies: []autoscalingv2.HPAScalingPolicy{
								{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 60},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "stabilization-window",
		},
		{
			name: "behavior with explicit stabilization window returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: int32Ptr(300),
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stabilizationWindowAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 {
				if got[0].ID != tt.wantID {
					t.Fatalf("expected finding ID %q, got %q", tt.wantID, got[0].ID)
				}
				if got[0].Severity != AuditWarning {
					t.Fatalf("expected warning severity, got %q", got[0].Severity)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. replicaRangeAuditRule
// ---------------------------------------------------------------------------

func TestReplicaRangeAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantIDs      []string
	}{
		{
			name: "max/min ratio > 10 returns range warning",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(1),
					MaxReplicas: 20,
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantIDs:      []string{"replica-range"},
		},
		{
			name: "max/min ratio <= 10 returns no range warning",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MinReplicas: int32Ptr(2),
					MaxReplicas: 10,
				},
			},
			minReplicas:  2,
			wantFindings: 0,
		},
		{
			name: "nil minReplicas returns info finding about default",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MaxReplicas: 5,
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantIDs:      []string{"replica-range"},
		},
		{
			name: "nil minReplicas with wide range returns two findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					MaxReplicas: 100,
				},
			},
			minReplicas:  1,
			wantFindings: 2,
			wantIDs:      []string{"replica-range", "replica-range"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replicaRangeAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			for i, wantID := range tt.wantIDs {
				if i < len(got) && got[i].ID != wantID {
					t.Fatalf("finding[%d]: expected ID %q, got %q", i, wantID, got[i].ID)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. behaviorPolicyAuditRule
// ---------------------------------------------------------------------------

func TestBehaviorPolicyAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantTitles   []string
	}{
		{
			name: "no behavior returns two info findings for scaleUp and scaleDown",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  1,
			wantFindings: 2,
			wantTitles: []string{
				"No explicit scaleUp policies configured",
				"No explicit scaleDown policies configured",
			},
		},
		{
			name: "only scaleUp policies returns one info finding for scaleDown",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							Policies: []autoscalingv2.HPAScalingPolicy{
								{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 60},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantTitles:   []string{"No explicit scaleDown policies configured"},
		},
		{
			name: "both policies set returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							Policies: []autoscalingv2.HPAScalingPolicy{
								{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 60},
							},
						},
						ScaleDown: &autoscalingv2.HPAScalingRules{
							Policies: []autoscalingv2.HPAScalingPolicy{
								{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 60},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "behavior exists but no policies on either side returns two findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{},
				},
			},
			minReplicas:  1,
			wantFindings: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := behaviorPolicyAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			for i, wantTitle := range tt.wantTitles {
				if i < len(got) && got[i].Title != wantTitle {
					t.Fatalf("finding[%d]: expected title %q, got %q", i, wantTitle, got[i].Title)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. metricCoverageAuditRule
// ---------------------------------------------------------------------------

func TestMetricCoverageAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantIDs      []string
	}{
		{
			name: "single metric returns one info finding",
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
									AverageUtilization: int32Ptr(70),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantIDs:      []string{"metric-coverage"},
		},
		{
			name: "only external metrics returns one info finding about resource safety",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
							},
						},
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "request-rate"},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantIDs:      []string{"metric-coverage"},
		},
		{
			name: "resource plus external metrics returns zero findings",
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
									AverageUtilization: int32Ptr(70),
								},
							},
						},
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "no metrics returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := metricCoverageAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			for i, wantID := range tt.wantIDs {
				if i < len(got) && got[i].ID != wantID {
					t.Fatalf("finding[%d]: expected ID %q, got %q", i, wantID, got[i].ID)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. toleranceAuditRule
// ---------------------------------------------------------------------------

func TestToleranceAuditRule(t *testing.T) {
	tolerance := resourcePtr(resource.MustParse("100m"))
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantID       string
	}{
		{
			name: "no behavior configured returns one info finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "tolerance",
		},
		{
			name: "behavior with no explicit tolerance returns one info finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							Policies: []autoscalingv2.HPAScalingPolicy{
								{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 60},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "tolerance",
		},
		{
			name: "explicit tolerance on scaleUp returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							Tolerance: tolerance,
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "explicit tolerance on scaleDown returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleDown: &autoscalingv2.HPAScalingRules{
							Tolerance: tolerance,
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toleranceAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 && got[0].ID != tt.wantID {
				t.Fatalf("expected finding ID %q, got %q", tt.wantID, got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. scaleToZeroAuditRule
// ---------------------------------------------------------------------------

func TestScaleToZeroAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantID       string
	}{
		{
			name: "minReplicas zero returns one warning finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  0,
			wantFindings: 1,
			wantID:       "scale-to-zero",
		},
		{
			name: "minReplicas one returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "minReplicas five returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  5,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaleToZeroAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 {
				if got[0].ID != tt.wantID {
					t.Fatalf("expected finding ID %q, got %q", tt.wantID, got[0].ID)
				}
				if got[0].Severity != AuditWarning {
					t.Fatalf("expected warning severity, got %q", got[0].Severity)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 7. resourceRequestAuditRule
// ---------------------------------------------------------------------------

func TestResourceRequestAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantID       string
	}{
		{
			name: "resource metrics present returns one info finding per resource metric",
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
									AverageUtilization: int32Ptr(70),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "resource-requests",
		},
		{
			name: "no resource metrics returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "two resource metrics returns two findings",
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
									AverageUtilization: int32Ptr(70),
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
			},
			minReplicas:  1,
			wantFindings: 2,
			wantID:       "resource-requests",
		},
		{
			name: "no metrics at all returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resourceRequestAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 && got[0].ID != tt.wantID {
				t.Fatalf("expected finding ID %q, got %q", tt.wantID, got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8. kedaAuditRule
// ---------------------------------------------------------------------------

func TestKEDAAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantID       string
	}{
		{
			name: "KEDA labels present returns one info finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "keda-hpa-worker",
					Namespace: "default",
					Labels: map[string]string{
						"scaledobject.keda.sh/name": "worker",
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "keda-managed",
		},
		{
			name: "KEDA annotation present returns one info finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-hpa",
					Namespace: "default",
					Annotations: map[string]string{
						"keda.sh/scaledobject": "my-scaledobject",
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "keda-managed",
		},
		{
			name: "no KEDA labels returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "regular-hpa",
					Namespace: "default",
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "keda-hpa- prefix name returns one info finding",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "keda-hpa-my-scaledobject",
					Namespace: "default",
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantID:       "keda-managed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kedaAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 && got[0].ID != tt.wantID {
				t.Fatalf("expected finding ID %q, got %q", tt.wantID, got[0].ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 9. targetUtilizationAuditRule
// ---------------------------------------------------------------------------

func TestTargetUtilizationAuditRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		minReplicas  int32
		wantFindings int
		wantTitles   []string
	}{
		{
			name: "high target utilization 95 percent returns one warning finding",
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
									AverageUtilization: int32Ptr(95),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantTitles:   []string{"High cpu target utilization (>90%)"},
		},
		{
			name: "low target utilization 20 percent returns one info finding",
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
									AverageUtilization: int32Ptr(20),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 1,
			wantTitles:   []string{"Low cpu target utilization (<30%)"},
		},
		{
			name: "normal target utilization 70 percent returns zero findings",
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
									AverageUtilization: int32Ptr(70),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "external metric type returns zero findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Metrics: []autoscalingv2.MetricSpec{
						{
							Type: autoscalingv2.ExternalMetricSourceType,
							External: &autoscalingv2.ExternalMetricSource{
								Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "boundary at exactly 90 percent returns zero findings",
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
									AverageUtilization: int32Ptr(90),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
		{
			name: "boundary at exactly 30 percent returns zero findings",
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
									AverageUtilization: int32Ptr(30),
								},
							},
						},
					},
				},
			},
			minReplicas:  1,
			wantFindings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := targetUtilizationAuditRule(tt.hpa, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			for i, wantTitle := range tt.wantTitles {
				if i < len(got) && got[i].Title != wantTitle {
					t.Fatalf("finding[%d]: expected title %q, got %q", i, wantTitle, got[i].Title)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 10. AuditHPA entry point
// ---------------------------------------------------------------------------

func TestAuditHPA(t *testing.T) {
	t.Run("nil HPA returns score 0", func(t *testing.T) {
		report := AuditHPA(nil, 1)
		if report.Score != 0 {
			t.Fatalf("expected score 0, got %d", report.Score)
		}
		if report.Summary != "HPA is nil" {
			t.Fatalf("expected nil summary, got %q", report.Summary)
		}
	})

	t.Run("well-configured HPA returns high score", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "good-hpa", Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment",
					Name: "my-app",
				},
				MinReplicas: int32Ptr(2),
				MaxReplicas: 10,
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
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
						},
					},
				},
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						Policies: []autoscalingv2.HPAScalingPolicy{
							{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 60},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(300),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 60},
						},
					},
				},
			},
		}
		report := AuditHPA(hpa, 2)
		// Well-configured HPA should still have some info findings (resource-requests, tolerance)
		// but score should be high (no warnings/critical)
		if report.Score < 80 {
			t.Fatalf("expected score >= 80 for well-configured HPA, got %d", report.Score)
		}
		if report.Namespace != "default" {
			t.Fatalf("expected namespace default, got %q", report.Namespace)
		}
		if report.Name != "good-hpa" {
			t.Fatalf("expected name good-hpa, got %q", report.Name)
		}
		if report.Target != "Deployment/my-app" {
			t.Fatalf("expected target Deployment/my-app, got %q", report.Target)
		}
	})

	t.Run("poorly configured HPA returns lower score", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-hpa", Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment",
					Name: "my-app",
				},
				MaxReplicas: 100,
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
		report := AuditHPA(hpa, 0)
		// Poorly configured: scale-to-zero (warning -10), high utilization (warning -10),
		// stabilization window (warning -10), nil minReplicas (info), wide range (warning -10),
		// behavior policies (2 info), tolerance (info), resource-requests (info)
		if report.Score >= 80 {
			t.Fatalf("expected score < 80 for poorly configured HPA, got %d", report.Score)
		}
	})

	t.Run("summary is generated", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment",
					Name: "my-app",
				},
				MinReplicas: int32Ptr(1),
				MaxReplicas: 5,
			},
		}
		report := AuditHPA(hpa, 1)
		if report.Summary == "" {
			t.Fatal("expected non-empty summary")
		}
	})

	t.Run("score never goes below zero", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment",
					Name: "my-app",
				},
				MaxReplicas: 200,
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ResourceMetricSourceType,
						Resource: &autoscalingv2.ResourceMetricSource{
							Name: corev1.ResourceCPU,
							Target: autoscalingv2.MetricTarget{
								Type:               autoscalingv2.UtilizationMetricType,
								AverageUtilization: int32Ptr(99),
							},
						},
					},
				},
			},
		}
		report := AuditHPA(hpa, 0)
		if report.Score < 0 {
			t.Fatalf("expected score >= 0, got %d", report.Score)
		}
	})
}

// ---------------------------------------------------------------------------
// 11. coreAuditRules count
// ---------------------------------------------------------------------------

func TestCoreAuditRules(t *testing.T) {
	rules := coreAuditRules()
	if len(rules) != 9 {
		t.Fatalf("expected 9 audit rules, got %d", len(rules))
	}
}

// ---------------------------------------------------------------------------
// Profile-specific rule tests
// ---------------------------------------------------------------------------

func TestProfileSpecificRules(t *testing.T) {
	tests := []struct {
		profile       AuditProfile
		wantRuleCount int
	}{
		{ProfileLatency, 2},
		{ProfileCost, 2},
		{ProfileBatch, 1},
		{ProfileKEDA, 2},
		{ProfileCritical, 2},
		{AuditProfile(""), 0},
		{AuditProfile("unknown"), 0},
	}
	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			rules := profileSpecificRules(tt.profile)
			if len(rules) != tt.wantRuleCount {
				t.Fatalf("expected %d profile rules for %q, got %d", tt.wantRuleCount, tt.profile, len(rules))
			}
		})
	}
}

func TestLatencyStabilizationRule(t *testing.T) {
	tests := []struct {
		name         string
		hpa          *autoscalingv2.HorizontalPodAutoscaler
		wantFindings int
	}{
		{
			name: "scaleUp stabilization > 60s returns warning",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: int32Ptr(120),
						},
					},
				},
			},
			wantFindings: 1,
		},
		{
			name: "scaleUp stabilization <= 60s returns no findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
						ScaleUp: &autoscalingv2.HPAScalingRules{
							StabilizationWindowSeconds: int32Ptr(30),
						},
					},
				},
			},
			wantFindings: 0,
		},
		{
			name: "no behavior returns no findings",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			},
			wantFindings: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latencyStabilizationRule(tt.hpa, 1)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 && got[0].Category != "profile-latency" {
				t.Fatalf("expected category profile-latency, got %q", got[0].Category)
			}
		})
	}
}

func TestCostMinReplicasRule(t *testing.T) {
	tests := []struct {
		name         string
		minReplicas  int32
		wantFindings int
	}{
		{name: "minReplicas 5 returns finding", minReplicas: 5, wantFindings: 1},
		{name: "minReplicas 2 returns no findings", minReplicas: 2, wantFindings: 0},
		{name: "minReplicas 1 returns no findings", minReplicas: 1, wantFindings: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := costMinReplicasRule(nil, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
		})
	}
}

func TestCriticalMaxHeadroomRule(t *testing.T) {
	tests := []struct {
		name         string
		current      int32
		maxReplicas  int32
		wantFindings int
	}{
		{name: "50% headroom OK", current: 4, maxReplicas: 8, wantFindings: 0},
		{name: "insufficient headroom", current: 8, maxReplicas: 10, wantFindings: 1},
		{name: "at max returns finding", current: 10, maxReplicas: 10, wantFindings: 1},
		{name: "zero current returns no findings", current: 0, maxReplicas: 10, wantFindings: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpa := &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test-hpa"},
				Spec:       autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: tt.maxReplicas},
				Status:     autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: tt.current},
			}
			got := criticalMaxHeadroomRule(hpa, 1)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
			if tt.wantFindings > 0 && got[0].Severity != AuditWarning {
				t.Fatalf("expected warning severity, got %q", got[0].Severity)
			}
		})
	}
}

func TestCriticalMinReplicasRule(t *testing.T) {
	tests := []struct {
		name         string
		minReplicas  int32
		wantFindings int
	}{
		{name: "minReplicas 1 returns warning", minReplicas: 1, wantFindings: 1},
		{name: "minReplicas 0 returns warning", minReplicas: 0, wantFindings: 1},
		{name: "minReplicas 2 returns no findings", minReplicas: 2, wantFindings: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := criticalMinReplicasRule(nil, tt.minReplicas)
			if len(got) != tt.wantFindings {
				t.Fatalf("expected %d findings, got %d", tt.wantFindings, len(got))
			}
		})
	}
}

func TestAuditHPAWithProfile(t *testing.T) {
	t.Run("empty profile behaves like AuditHPA", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				MinReplicas: int32Ptr(2),
				MaxReplicas: 10,
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
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: int32Ptr(300),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 60},
						},
					},
					ScaleUp: &autoscalingv2.HPAScalingRules{
						Policies: []autoscalingv2.HPAScalingPolicy{
							{Type: autoscalingv2.PodsScalingPolicy, Value: 4, PeriodSeconds: 60},
						},
					},
				},
			},
		}
		reportDefault := AuditHPA(hpa, 2)
		reportEmpty := AuditHPAWithProfile(hpa, 2, "")
		if reportDefault.Score != reportEmpty.Score {
			t.Fatalf("expected same score, got %d vs %d", reportDefault.Score, reportEmpty.Score)
		}
	})

	t.Run("critical profile adds extra findings", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				MinReplicas: int32Ptr(1),
				MaxReplicas: 5,
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
			},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 4},
		}
		report := AuditHPAWithProfile(hpa, 1, ProfileCritical)
		found := false
		for _, f := range report.Findings {
			if f.ID == "critical-min-replicas" || f.ID == "critical-max-headroom" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected at least one critical profile finding")
		}
		if report.Profile != ProfileCritical {
			t.Fatalf("expected profile critical, got %q", report.Profile)
		}
	})
}

// ---------------------------------------------------------------------------
// 12. WriteAuditText
// ---------------------------------------------------------------------------

func TestWriteAuditText(t *testing.T) {
	t.Run("with findings outputs severity title and description", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "web-hpa",
			Target:    "Deployment/web",
			Score:     80,
			Summary:   "Found 0 critical, 1 warnings, 0 informational findings (score: 80/100)",
			Findings: []AuditFinding{
				{
					ID:          "stabilization-window",
					Title:       "Stabilization window not explicitly configured",
					Description: "scaleDown.stabilizationWindowSeconds is unset.",
					Severity:    AuditWarning,
					Category:    "stabilization",
					Current:     "unset (default 300s)",
					Recommended: "Set stabilizationWindowSeconds explicitly",
				},
			},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		for _, want := range []string{
			"warning",
			"Stabilization window not explicitly configured",
			"scaleDown.stabilizationWindowSeconds is unset.",
			"stabilization-window",
			"unset (default 300s)",
			"Set stabilizationWindowSeconds explicitly",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("expected %q in output, got:\n%s", want, output)
			}
		}
	})

	t.Run("no findings outputs no findings message", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "perfect-hpa",
			Target:    "Deployment/web",
			Score:     100,
			Summary:   "No best-practice issues found.",
			Findings:  []AuditFinding{},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "No findings.") {
			t.Fatalf("expected 'No findings.' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "100/100") {
			t.Fatalf("expected score in output, got:\n%s", output)
		}
	})

	t.Run("nil provider uses English defaults", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "test-hpa",
			Target:    "Deployment/test",
			Score:     90,
			Summary:   "No best-practice issues found.",
			Findings:  []AuditFinding{},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "Target:") {
			t.Fatalf("expected English default label 'Target:' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Compliance Score:") {
			t.Fatalf("expected English default label 'Compliance Score:' in output, got:\n%s", output)
		}
	})

	t.Run("finding with command outputs command line", func(t *testing.T) {
		report := &AuditReport{
			Namespace: "default",
			Name:      "web-hpa",
			Target:    "Deployment/web",
			Score:     80,
			Summary:   "Found 0 critical, 1 warnings, 0 informational findings (score: 80/100)",
			Findings: []AuditFinding{
				{
					ID:          "stabilization-window",
					Title:       "Stabilization window not explicitly configured",
					Description: "scaleDown.stabilizationWindowSeconds is unset.",
					Severity:    AuditWarning,
					Command:     "kubectl patch hpa web-hpa -n default --type=merge -p '{}' --dry-run=server",
				},
			},
		}

		var buf bytes.Buffer
		if err := WriteAuditText(&buf, report, nil); err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "Command:") {
			t.Fatalf("expected 'Command:' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "kubectl patch") {
			t.Fatalf("expected kubectl command in output, got:\n%s", output)
		}
	})
}
