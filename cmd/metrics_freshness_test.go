package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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
			KEDAInfo: &hpakeda.Analysis{
				ScaledObjectName: "web",
				Triggers: []hpakeda.TriggerSummary{
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

func TestDiscoverMetricsAPIPrefersCustomV1Beta2AndFallsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources []*metav1.APIResourceList
		want      string
	}{
		{
			name: "prefers v1beta2",
			resources: []*metav1.APIResourceList{
				{GroupVersion: "custom.metrics.k8s.io/v1beta1"},
				{GroupVersion: "custom.metrics.k8s.io/v1beta2"},
			},
			want: "custom.metrics.k8s.io/v1beta2",
		},
		{
			name: "falls back to v1beta1",
			resources: []*metav1.APIResourceList{
				{GroupVersion: "custom.metrics.k8s.io/v1beta1"},
			},
			want: "custom.metrics.k8s.io/v1beta1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fakeClient := testutil.NewFakeClient()
			fakeClient.Discovery().(*fakediscovery.FakeDiscovery).Resources = tt.resources
			status := discoverMetricsAPI(
				&kube.Client{Interface: fakeClient, Namespace: "default"},
				"custom.metrics.k8s.io",
			)
			if !status.Available || status.Message != tt.want {
				t.Fatalf("discoverMetricsAPI() = %+v, want available %q", status, tt.want)
			}
		})
	}
}

func TestPodMetricSamplesRetainContainerAndFilterContainerResource(t *testing.T) {
	t.Parallel()

	var list podMetricsListJSON
	if err := json.Unmarshal([]byte(`{
		"items": [{
			"timestamp": "2026-07-31T00:00:00Z",
			"window": "30s",
			"containers": [
				{"name": "app", "usage": {"cpu": "100m"}},
				{"name": "sidecar", "usage": {"cpu": "20m"}}
			]
		}]
	}`), &list); err != nil {
		t.Fatalf("unmarshal PodMetrics fixture: %v", err)
	}
	samples := podMetricSamplesFromList(list)
	if len(samples) != 2 || samples[0].Container == "" || samples[1].Container == "" {
		t.Fatalf("podMetricSamplesFromList() = %+v, want container names", samples)
	}

	newer := time.Date(2026, 7, 31, 0, 5, 0, 0, time.UTC)
	older := newer.Add(-5 * time.Minute)
	samples = []podMetricSample{
		{Container: "app", Resource: corev1.ResourceCPU, Timestamp: newer, Window: "30s"},
		{Container: "sidecar", Resource: corev1.ResourceCPU, Timestamp: older, Window: "30s"},
	}
	sidecar, found := latestPodMetricSample(samples, corev1.ResourceCPU, "sidecar")
	if !found || !sidecar.Timestamp.Equal(older) {
		t.Fatalf("latestPodMetricSample(sidecar) = %+v/%v, want the sidecar sample", sidecar, found)
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ContainerResourceMetricSourceType,
				ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
					Name:      corev1.ResourceCPU,
					Container: "sidecar",
				},
			}},
		},
	}
	resourceName, containerName, ok := resourceIdentityForFreshnessEntry(
		hpa,
		0,
		hpaanalysis.MetricFreshness{Name: "cpu", Type: "ContainerResource"},
	)
	if !ok || resourceName != corev1.ResourceCPU || containerName != "sidecar" {
		t.Fatalf("resourceIdentityForFreshnessEntry() = (%q, %q, %v), want (cpu, sidecar, true)",
			resourceName, containerName, ok)
	}
}

func TestStaleWindowThreshold(t *testing.T) {
	tests := []struct {
		name   string
		window string
		want   time.Duration
	}{
		{name: "valid window doubles it", window: "60s", want: 2 * time.Minute},
		{name: "invalid window falls back", window: "not-a-duration", want: 2 * time.Minute},
		{name: "non-positive window falls back", window: "-5s", want: 2 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staleWindowThreshold(tt.window); got != tt.want {
				t.Fatalf("staleWindowThreshold(%q) = %v, want %v", tt.window, got, tt.want)
			}
		})
	}
}

func TestFormatAgeForEvidence(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{in: 90 * time.Second, want: "1m30s"},
		{in: 1500 * time.Millisecond, want: "2s"},
		{in: -3 * time.Second, want: "0s"}, // negative clamps to zero
	}
	for _, tt := range tests {
		if got := formatAgeForEvidence(tt.in); got != tt.want {
			t.Fatalf("formatAgeForEvidence(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
