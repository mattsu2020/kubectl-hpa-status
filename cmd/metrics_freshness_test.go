package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
)

func TestEnrichMetricFreshnessAddsAPIDiscoveryStatus(t *testing.T) {
	hpa := testutil.BuildHPA("default", "web", testutil.WithResourceMetric("cpu", 80, 75))
	fakeClient := testutil.NewFakeClient(hpa)
	fakeClient.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{
		{GroupVersion: "metrics.k8s.io/v1beta1"},
	}
	client := &kube.Client{Interface: fakeClient, Namespace: "default"}
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			MetricFreshnessEntries: hpaanalysis.AnalyzeMetricFreshness(hpa, nil),
		},
	}

	enrichMetricFreshness(context.Background(), client, hpa, &report)

	if len(report.Analysis.MetricFreshnessEntries) != 1 {
		t.Fatalf("expected one freshness entry, got %d", len(report.Analysis.MetricFreshnessEntries))
	}
	entry := report.Analysis.MetricFreshnessEntries[0]
	if entry.APIServiceAvailable == nil || !*entry.APIServiceAvailable {
		t.Fatalf("expected APIServiceAvailable=true, got %#v", entry.APIServiceAvailable)
	}
	if entry.APIServiceMessage != "metrics.k8s.io/v1beta1" {
		t.Fatalf("unexpected APIServiceMessage: %q", entry.APIServiceMessage)
	}
}

func TestEnrichMetricFreshnessAddsKEDAEvidence(t *testing.T) {
	hpa := testutil.BuildHPA("production", "web",
		testutil.WithExternalMetric("keda-http-requests", "10"),
	)
	client := &kube.Client{Interface: testutil.NewFakeClient(hpa), Namespace: "production"}
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			MetricFreshnessEntries: hpaanalysis.AnalyzeMetricFreshness(hpa, nil),
			KEDAInfo: &hpaanalysis.KEDAAnalysis{
				ScaledObjectName: "web",
				Triggers: []hpaanalysis.KEDATriggerSummary{
					{
						Type:       "http",
						Name:       "http-requests",
						Status:     "Inactive",
						MetricName: "keda-http-requests",
						Message:    "authentication failed",
					},
				},
			},
		},
	}

	enrichMetricFreshness(context.Background(), client, hpa, &report)

	entry := report.Analysis.MetricFreshnessEntries[0]
	evidence := strings.Join(entry.Evidence, "\n")
	if !strings.Contains(evidence, `KEDA ScaledObject "web"`) {
		t.Fatalf("expected KEDA ScaledObject evidence, got %v", entry.Evidence)
	}
	if !strings.Contains(evidence, "status=Inactive") {
		t.Fatalf("expected inactive trigger evidence, got %v", entry.Evidence)
	}
	if !strings.Contains(strings.Join(entry.NextSteps, "\n"), "kubectl get scaledobject web -n production") {
		t.Fatalf("expected scaledobject next step, got %v", entry.NextSteps)
	}
	if entry.Risk != "KEDA trigger is inactive or authentication is failing" {
		t.Fatalf("unexpected risk: %q", entry.Risk)
	}
}

func TestLatestMetricFailureEvent(t *testing.T) {
	entry := hpaanalysis.MetricFreshness{Name: "queue_depth", Type: string(autoscalingv2.ExternalMetricSourceType)}
	events := []hpaanalysis.Event{
		{Reason: "FailedGetResourceMetric", Message: "cpu missing"},
		{Reason: "FailedGetExternalMetric", Message: "queue_depth unavailable"},
	}

	got := latestMetricFailureEvent(events, entry)
	if got == nil || got.Reason != "FailedGetExternalMetric" {
		t.Fatalf("expected FailedGetExternalMetric, got %#v", got)
	}
}
