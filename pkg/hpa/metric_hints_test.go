package hpa

import (
	"strings"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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
			freshness:      []MetricFreshness{{Name: "http_requests", Type: "Pods", Source: "custom.metrics.k8s.io", APIServiceAvailable: ptr.To(false)}},
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

func TestAnalyzeMetricHintsCorrelatesFreshnessByCanonicalSelectorIdentity(t *testing.T) {
	t.Parallel()

	target := resource.MustParse("100")
	externalSpec := func(queue string) autoscalingv2.MetricSpec {
		return autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "queue_depth",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"queue": queue},
					},
				},
				Target: autoscalingv2.MetricTarget{
					Type:  autoscalingv2.ValueMetricType,
					Value: &target,
				},
			},
		}
	}
	hpa := buildExternalMetricHPA("unused")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{
		externalSpec("critical"),
		externalSpec("bulk"),
	}

	freshness := AnalyzeMetricFreshness(hpa, nil)
	if len(freshness) != 2 {
		t.Fatalf("AnalyzeMetricFreshness() returned %d entries, want 2", len(freshness))
	}
	freshness[0].Status = string(FreshnessStale)
	freshness[1].Status = string(FreshnessOK)

	report := AnalyzeMetricHints(hpa, nil, freshness, nil)
	staleHints := 0
	for _, hint := range report.Hints {
		if hint.Pattern == "external-metric-stale" {
			staleHints++
		}
	}
	if staleHints != 1 {
		t.Fatalf("selector-specific stale hints = %d, want 1: %+v", staleHints, report.Hints)
	}
}

func TestAnalyzeMetricHintsCorrelatesContractByCanonicalSelectorIdentity(t *testing.T) {
	t.Parallel()

	target := resource.MustParse("100")
	externalSpec := func(queue string) autoscalingv2.MetricSpec {
		return autoscalingv2.MetricSpec{
			Type: autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{
				Metric: autoscalingv2.MetricIdentifier{
					Name: "queue_depth",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"queue": queue},
					},
				},
				Target: autoscalingv2.MetricTarget{
					Type:  autoscalingv2.ValueMetricType,
					Value: &target,
				},
			},
		}
	}
	critical := externalSpec("critical")
	bulk := externalSpec("bulk")
	hpa := buildExternalMetricHPA("unused")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{critical, bulk}

	criticalID, err := MetricIDFromSpec(critical)
	if err != nil {
		t.Fatalf("MetricIDFromSpec(critical): %v", err)
	}
	bulkID, err := MetricIDFromSpec(bulk)
	if err != nil {
		t.Fatalf("MetricIDFromSpec(bulk): %v", err)
	}
	contract := AnalyzeMetricContract(MetricContractInput{
		Metrics: []MetricContractMetric{
			WithMetricContractIdentity(MetricContractMetric{
				Type:           string(autoscalingv2.ExternalMetricSourceType),
				Name:           "queue_depth",
				APIGroup:       "external.metrics.k8s.io/v1beta1",
				HasCurrentData: false,
			}, criticalID),
			WithMetricContractIdentity(MetricContractMetric{
				Type:           string(autoscalingv2.ExternalMetricSourceType),
				Name:           "queue_depth",
				APIGroup:       "external.metrics.k8s.io/v1beta1",
				HasCurrentData: true,
			}, bulkID),
		},
		APIServices: map[string]APIServiceStatus{
			"external.metrics.k8s.io/v1beta1": {Available: true},
		},
	})

	report := AnalyzeMetricHints(hpa, nil, nil, contract)
	missingHints := 0
	for _, hint := range report.Hints {
		if hint.Pattern == "missing-metric-in-status" {
			missingHints++
		}
	}
	if missingHints != 1 {
		t.Fatalf("selector-specific missing hints = %d, want 1: %+v", missingHints, report.Hints)
	}
}

// buildExternalMetricHPA creates an HPA with a single external metric and no status.
func buildExternalMetricHPA(metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithMinMax(1, 10),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: metricName},
			Target: autoscalingv2.MetricTarget{Value: ptr.To(resource.MustParse("100"))},
		},
	}}
	return hpa
}

// buildExternalMetricHPAWithStatus creates an HPA with an external metric and matching status.
func buildExternalMetricHPAWithStatus(metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := buildExternalMetricHPA(metricName)
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricStatus{
			Metric:  autoscalingv2.MetricIdentifier{Name: metricName},
			Current: autoscalingv2.MetricValueStatus{Value: ptr.To(resource.MustParse("50"))},
		},
	}}
	return hpa
}

// buildPodsMetricHPA creates an HPA with a single Pods metric.
func buildPodsMetricHPA(metricName string) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithMinMax(1, 10),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.PodsMetricSourceType,
		Pods: &autoscalingv2.PodsMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: metricName},
			Target: autoscalingv2.MetricTarget{AverageValue: ptr.To(resource.MustParse("100"))},
		},
	}}
	return hpa
}

// buildResourceMetricHPAWithStatus creates an HPA with a resource (CPU) metric and matching status.
func buildResourceMetricHPAWithStatus() *autoscalingv2.HorizontalPodAutoscaler {
	hpa := testutil.BuildHPA("default", "web",
		testutil.WithMinMax(1, 10),
		testutil.WithScaleTargetRef("Deployment", "web"),
	)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: "cpu",
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: ptr.To(int32(80)),
			},
		},
	}}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{
			Name:    "cpu",
			Current: autoscalingv2.MetricValueStatus{AverageUtilization: ptr.To(int32(50))},
		},
	}}
	return hpa
}
