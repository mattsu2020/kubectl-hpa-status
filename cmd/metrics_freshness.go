package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func enrichMetricFreshness(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if client == nil || hpa == nil || report == nil || len(report.Analysis.MetricFreshnessEntries) == 0 {
		return
	}

	apiCache := map[string]apiDiscoveryStatus{}
	for i := range report.Analysis.MetricFreshnessEntries {
		entry := &report.Analysis.MetricFreshnessEntries[i]
		if entry.Source != "" {
			status, ok := apiCache[entry.Source]
			if !ok {
				status = discoverMetricsAPI(client, entry.Source)
				apiCache[entry.Source] = status
			}
			entry.APIServiceAvailable = &status.Available
			entry.APIServiceMessage = status.Message
			if !status.Available {
				appendUniqueEvidence(entry, fmt.Sprintf("%s is not available through API discovery: %s", entry.Source, status.Message))
				if entry.Risk == "" || entry.Status == string(hpaanalysis.FreshnessOK) {
					entry.Status = string(hpaanalysis.FreshnessStale)
					entry.Risk = "metrics API is unavailable or the adapter returned no fresh value"
				}
			}
		}

		if event := latestMetricFailureEvent(report.Events, *entry); event != nil {
			entry.LastEvent = event
		}
	}

	enrichResourceMetricSamples(ctx, client, hpa, report)
	enrichKEDAFreshness(hpa.Namespace, report)
}

type apiDiscoveryStatus struct {
	Available bool
	Message   string
}

func discoverMetricsAPI(client *kube.Client, source string) apiDiscoveryStatus {
	groupVersion := metricsAPIGroupVersion(source)
	if groupVersion == "" {
		return apiDiscoveryStatus{Available: false, Message: "unknown metrics API source"}
	}
	_, err := client.Interface.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return apiDiscoveryStatus{Available: false, Message: err.Error()}
	}
	return apiDiscoveryStatus{Available: true, Message: groupVersion}
}

func metricsAPIGroupVersion(source string) string {
	switch source {
	case "metrics.k8s.io":
		return "metrics.k8s.io/v1beta1"
	case "custom.metrics.k8s.io":
		return "custom.metrics.k8s.io/v1beta1"
	case "external.metrics.k8s.io":
		return "external.metrics.k8s.io/v1beta1"
	default:
		return ""
	}
}

func latestMetricFailureEvent(events []hpaanalysis.Event, entry hpaanalysis.MetricFreshness) *hpaanalysis.Event {
	var latest *hpaanalysis.Event
	for i := range events {
		event := events[i]
		if !isMetricFailureReason(event.Reason) {
			continue
		}
		if !freshnessEventMatchesType(event.Reason, entry.Type) {
			continue
		}
		name := strings.ToLower(entry.Name)
		if name != "" && !strings.Contains(strings.ToLower(event.Message), name) {
			continue
		}
		if latest == nil || event.Timestamp.After(latest.Timestamp) {
			evtCopy := event
			latest = &evtCopy
		}
	}
	return latest
}

func isMetricFailureReason(reason string) bool {
	return strings.HasPrefix(reason, "FailedGet") || strings.HasPrefix(reason, "FailedMetric")
}

func freshnessEventMatchesType(reason, metricType string) bool {
	reason = strings.ToLower(reason)
	switch metricType {
	case "Resource", "ContainerResource":
		return strings.Contains(reason, "resource")
	case "External":
		return strings.Contains(reason, "external")
	case "Pods":
		return strings.Contains(reason, "pods") || strings.Contains(reason, "pod")
	case "Object":
		return strings.Contains(reason, "object")
	default:
		return true
	}
}

func enrichResourceMetricSamples(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport) {
	if !hasResourceFreshnessEntry(report.Analysis.MetricFreshnessEntries) {
		return
	}
	selector, err := resolveScaleTargetSelector(ctx, client, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || selector == "" {
		return
	}
	samples, err := fetchPodMetricSamples(ctx, client, hpa.Namespace, selector)
	if err != nil || len(samples) == 0 {
		return
	}

	now := time.Now()
	for i := range report.Analysis.MetricFreshnessEntries {
		entry := &report.Analysis.MetricFreshnessEntries[i]
		resourceName, ok := resourceNameForFreshnessEntry(hpa, i, *entry)
		if !ok {
			continue
		}
		sample, ok := latestPodMetricSample(samples, resourceName)
		if !ok {
			continue
		}
		entry.LastSeen = &metav1.Time{Time: sample.Timestamp}
		entry.Age = now.Sub(sample.Timestamp)
		if sample.Window != "" {
			entry.Window = sample.Window
		}
		if sample.Window != "" && entry.Age > staleWindowThreshold(sample.Window) {
			entry.Status = string(hpaanalysis.FreshnessStale)
			entry.Risk = "metrics-server returned an old PodMetrics sample"
			appendUniqueEvidence(entry, fmt.Sprintf("PodMetrics timestamp is %s old", formatAgeForEvidence(entry.Age)))
		}
	}
}

func hasResourceFreshnessEntry(entries []hpaanalysis.MetricFreshness) bool {
	for _, entry := range entries {
		if entry.Type == "Resource" || entry.Type == "ContainerResource" {
			return true
		}
	}
	return false
}

type podMetricSample struct {
	Resource  corev1.ResourceName
	Timestamp time.Time
	Window    string
}

type podMetricsListJSON struct {
	Items []struct {
		Timestamp  metav1.Time `json:"timestamp"`
		Window     string      `json:"window"`
		Containers []struct {
			Usage corev1.ResourceList `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

func fetchPodMetricSamples(ctx context.Context, client *kube.Client, namespace, selector string) ([]podMetricSample, error) {
	restClient := client.Interface.Discovery().RESTClient()
	if restClient == nil {
		return nil, fmt.Errorf("discovery REST client is unavailable")
	}
	raw, err := restClient.Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods").
		Param("labelSelector", selector).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var list podMetricsListJSON
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var samples []podMetricSample
	for _, item := range list.Items {
		if item.Timestamp.IsZero() {
			continue
		}
		for _, container := range item.Containers {
			for resourceName := range container.Usage {
				samples = append(samples, podMetricSample{
					Resource:  resourceName,
					Timestamp: item.Timestamp.Time,
					Window:    item.Window,
				})
			}
		}
	}
	return samples, nil
}

func resourceNameForFreshnessEntry(hpa *autoscalingv2.HorizontalPodAutoscaler, index int, entry hpaanalysis.MetricFreshness) (corev1.ResourceName, bool) {
	if entry.Type != "Resource" && entry.Type != "ContainerResource" {
		return "", false
	}
	if index >= 0 && index < len(hpa.Spec.Metrics) {
		spec := hpa.Spec.Metrics[index]
		if spec.Type == autoscalingv2.ResourceMetricSourceType && spec.Resource != nil {
			return spec.Resource.Name, true
		}
		if spec.Type == autoscalingv2.ContainerResourceMetricSourceType && spec.ContainerResource != nil {
			return spec.ContainerResource.Name, true
		}
	}
	return corev1.ResourceName(entry.Name), entry.Name != ""
}

func latestPodMetricSample(samples []podMetricSample, resourceName corev1.ResourceName) (podMetricSample, bool) {
	var latest podMetricSample
	found := false
	for _, sample := range samples {
		if sample.Resource != resourceName {
			continue
		}
		if !found || sample.Timestamp.After(latest.Timestamp) {
			latest = sample
			found = true
		}
	}
	return latest, found
}

func staleWindowThreshold(window string) time.Duration {
	parsed, err := time.ParseDuration(window)
	if err != nil || parsed <= 0 {
		return 2 * time.Minute
	}
	return parsed * 2
}

func formatAgeForEvidence(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return d.Round(time.Second).String()
}

func enrichKEDAFreshness(namespace string, report *hpaanalysis.StatusReport) {
	keda := report.Analysis.KEDAInfo
	if keda == nil {
		return
	}
	for i := range report.Analysis.MetricFreshnessEntries {
		entry := &report.Analysis.MetricFreshnessEntries[i]
		if entry.Type != "External" {
			continue
		}
		appendUniqueEvidence(entry, fmt.Sprintf("KEDA ScaledObject %q is linked to this HPA", keda.ScaledObjectName))
		for _, trigger := range keda.Triggers {
			if trigger.MetricName != "" && !strings.EqualFold(trigger.MetricName, entry.Name) {
				continue
			}
			detail := fmt.Sprintf("KEDA trigger %q", trigger.Type)
			if trigger.Name != "" {
				detail += fmt.Sprintf(" (%s)", trigger.Name)
			}
			if trigger.Status != "" {
				detail += fmt.Sprintf(" status=%s", trigger.Status)
			}
			if trigger.Message != "" {
				detail += fmt.Sprintf(": %s", trigger.Message)
			}
			appendUniqueEvidence(entry, detail)
			if strings.EqualFold(trigger.Status, "Inactive") {
				entry.Risk = "KEDA trigger is inactive or authentication is failing"
			}
		}
		step := fmt.Sprintf("kubectl get scaledobject %s -n %s", keda.ScaledObjectName, namespace)
		appendUniqueNextStep(entry, step)
	}
}

func appendUniqueEvidence(entry *hpaanalysis.MetricFreshness, value string) {
	if value == "" {
		return
	}
	for _, existing := range entry.Evidence {
		if existing == value {
			return
		}
	}
	entry.Evidence = append(entry.Evidence, value)
}

func appendUniqueNextStep(entry *hpaanalysis.MetricFreshness, value string) {
	if value == "" {
		return
	}
	for _, existing := range entry.NextSteps {
		if existing == value {
			return
		}
	}
	entry.NextSteps = append(entry.NextSteps, value)
}
