package hpa

import (
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestDiagnoseAdapterResourceOnly(t *testing.T) {
	hpa := kube.BuildHPA("default", "web", kube.WithResourceMetric("cpu", 80, 60))

	got := DiagnoseAdapter(hpa, nil, nil)
	if got.AdapterType != "none" {
		t.Fatalf("adapter type = %q, want none", got.AdapterType)
	}
	if !got.EndpointHealthy {
		t.Fatal("resource-only HPA should not report adapter endpoint unhealthy")
	}
}

func TestDiagnoseAdapterExternalMetric(t *testing.T) {
	hpa := kube.BuildHPA("default", "web")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.ExternalMetricSourceType,
		External: &autoscalingv2.ExternalMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"},
			Target: autoscalingv2.MetricTarget{
				Type:  autoscalingv2.ValueMetricType,
				Value: resource.NewQuantity(100, resource.DecimalSI),
			},
		},
	}}

	got := DiagnoseAdapter(hpa, nil, nil)
	if got.AdapterType != "external" {
		t.Fatalf("adapter type = %q, want external", got.AdapterType)
	}
	if len(got.QueryProposals) != 1 {
		t.Fatalf("expected one query proposal, got %+v", got.QueryProposals)
	}
	if !strings.Contains(got.QueryProposals[0].ProposedQuery, "external.metrics.k8s.io") {
		t.Fatalf("unexpected query proposal: %s", got.QueryProposals[0].ProposedQuery)
	}
}

func TestDiagnoseAdapterFreshnessError(t *testing.T) {
	hpa := kube.BuildHPA("default", "web")
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
		Type: autoscalingv2.PodsMetricSourceType,
		Pods: &autoscalingv2.PodsMetricSource{
			Metric: autoscalingv2.MetricIdentifier{Name: "http_requests"},
			Target: autoscalingv2.MetricTarget{
				Type:         autoscalingv2.AverageValueMetricType,
				AverageValue: resource.NewQuantity(10, resource.DecimalSI),
			},
		},
	}}
	freshness := []MetricFreshness{{
		Name:   "http_requests",
		Type:   string(autoscalingv2.PodsMetricSourceType),
		Status: string(FreshnessMissing),
	}}

	got := DiagnoseAdapter(hpa, freshness, nil)
	if got.EndpointHealthy {
		t.Fatal("missing custom metric freshness should mark endpoint unhealthy")
	}
	if len(got.Checks) < 2 {
		t.Fatalf("expected metric reference and freshness checks, got %+v", got.Checks)
	}
}
