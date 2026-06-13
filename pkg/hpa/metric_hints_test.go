package hpa

import (
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzeMetricHints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		hpa            *autoscalingv2.HorizontalPodAutoscaler
		events         []Event
		freshness      []MetricFreshness
		contract       *MetricContractReport
		wantNil        bool
		wantMinHints   int
		wantSummaryHas string
		assertHint     func(t *testing.T, hints []MetricHint)
	}{
		{
			name:    "nil HPA returns nil",
			hpa:     nil,
			wantNil: true,
		},
		{
			name:           "healthy metrics - no hints",
			hpa:            buildExternalMetricHPAWithStatus("queue_depth"),
			wantMinHints:   0,
			wantSummaryHas: "healthy",
		},
		{
			name: "external metric missing with failed events",
			hpa:  buildExternalMetricHPA("queue_depth"),
			events: []Event{
				{Reason: "FailedGetExternalMetric", Message: "unable to get metric queue_depth", Timestamp: time.Now()},
			},
			wantMinHints:   1,
			wantSummaryHas: "issue",
			assertHint: func(t *testing.T, hints []MetricHint) {
				t.Helper()
				found := false
				for _, h := range hints {
					if h.Severity == "error" && strings.Contains(h.Pattern, "external-metric-missing") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected at least one hint with severity 'error' and pattern containing 'external-metric-missing'")
				}
			},
		},
		{
			name:           "external metric stale",
			hpa:            buildExternalMetricHPAWithStatus("queue_depth"),
			freshness:      []MetricFreshness{{Name: "queue_depth", Type: "External", Status: "Stale"}},
			wantMinHints:   1,
			wantSummaryHas: "issue",
			assertHint: func(t *testing.T, hints []MetricHint) {
				t.Helper()
				found := false
				for _, h := range hints {
					if strings.Contains(h.Pattern, "external-metric-stale") {
						found = true
						if h.Severity != "warning" {
							t.Errorf("stale hint severity = %q, want 'warning'", h.Severity)
						}
						break
					}
				}
				if !found {
					t.Error("expected at least one hint about stale metric")
				}
			},
		},
		{
			name:           "custom metrics API unavailable",
			hpa:            buildPodsMetricHPA("http_requests"),
			freshness:      []MetricFreshness{{Name: "http_requests", Type: "Pods", Source: "custom.metrics.k8s.io", APIServiceAvailable: boolPtrForHintTest(false)}},
			contract:       &MetricContractReport{Checks: []MetricContractCheck{{MetricType: "Pods", MetricName: "http_requests", Status: "error"}}},
			wantMinHints:   1,
			wantSummaryHas: "issue",
			assertHint: func(t *testing.T, hints []MetricHint) {
				t.Helper()
				found := false
				for _, h := range hints {
					if strings.Contains(h.Pattern, "custom-api-service-unavailable") {
						found = true
						if h.Severity != "error" {
							t.Errorf("API unavailable hint severity = %q, want 'error'", h.Severity)
						}
						break
					}
				}
				if !found {
					t.Error("expected at least one hint about API service unavailable")
				}
			},
		},
		{
			name:           "no hints for resource metrics with matching status",
			hpa:            buildResourceMetricHPAWithStatus(),
			wantMinHints:   0,
			wantSummaryHas: "healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := AnalyzeMetricHints(tt.hpa, tt.events, tt.freshness, tt.contract)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil MetricHintsReport, got nil")
			}

			if len(got.Hints) < tt.wantMinHints {
				t.Fatalf("expected at least %d hints, got %d: %+v", tt.wantMinHints, len(got.Hints), got.Hints)
			}

			if !strings.Contains(strings.ToLower(got.Summary), strings.ToLower(tt.wantSummaryHas)) {
				t.Errorf("summary %q should contain %q", got.Summary, tt.wantSummaryHas)
			}

			if tt.assertHint != nil {
				tt.assertHint(t, got.Hints)
			}
		})
	}
}

// buildExternalMetricHPA creates an HPA with a single external metric and no status.
func buildExternalMetricHPA(metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    int32PtrForHintTest(1),
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: metricName},
					Target: autoscalingv2.MetricTarget{Value: quantityPtrForHintTest(resource.MustParse("100"))},
				},
			}},
		},
	}
}

// buildExternalMetricHPAWithStatus creates an HPA with an external metric and matching status.
func buildExternalMetricHPAWithStatus(metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    int32PtrForHintTest(1),
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: metricName},
					Target: autoscalingv2.MetricTarget{Value: quantityPtrForHintTest(resource.MustParse("100"))},
				},
			}},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.ExternalMetricSourceType,
				External: &autoscalingv2.ExternalMetricStatus{
					Metric:  autoscalingv2.MetricIdentifier{Name: metricName},
					Current: autoscalingv2.MetricValueStatus{Value: quantityPtrForHintTest(resource.MustParse("50"))},
				},
			}},
		},
	}
}

// buildPodsMetricHPA creates an HPA with a single Pods metric.
func buildPodsMetricHPA(metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    int32PtrForHintTest(1),
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: metricName},
					Target: autoscalingv2.MetricTarget{AverageValue: quantityPtrForHintTest(resource.MustParse("100"))},
				},
			}},
		},
	}
}

// buildResourceMetricHPAWithStatus creates an HPA with a resource (CPU) metric and matching status.
func buildResourceMetricHPAWithStatus() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MinReplicas:    int32PtrForHintTest(1),
			MaxReplicas:    10,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: "cpu",
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: int32PtrForHintTest(80),
					},
				},
			}},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name:    "cpu",
					Current: autoscalingv2.MetricValueStatus{AverageUtilization: int32PtrForHintTest(50)},
				},
			}},
		},
	}
}

func int32PtrForHintTest(v int32) *int32 { return &v }

func boolPtrForHintTest(v bool) *bool { return &v }

func quantityPtrForHintTest(q resource.Quantity) *resource.Quantity { return &q }
