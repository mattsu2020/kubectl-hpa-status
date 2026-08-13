package metricsapi

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodMetricsList is the subset of metrics.k8s.io used by all diagnostics.
type PodMetricsList struct {
	Items []PodMetrics `json:"items"`
}

// PodMetrics is one pod's resource usage observation.
type PodMetrics struct {
	Metadata   metav1.ObjectMeta  `json:"metadata"`
	Timestamp  metav1.Time        `json:"timestamp"`
	Window     string             `json:"window"`
	Containers []ContainerMetrics `json:"containers"`
}

// ContainerMetrics is one container's resource usage observation.
type ContainerMetrics struct {
	Name  string              `json:"name"`
	Usage corev1.ResourceList `json:"usage"`
}

// ListPodMetrics performs the canonical bounded REST request and decoding.
func ListPodMetrics(ctx context.Context, client kubernetes.Interface, namespace, selector string) (PodMetricsList, error) {
	if client == nil || client.Discovery().RESTClient() == nil {
		return PodMetricsList{}, fmt.Errorf("discovery REST client is unavailable")
	}
	raw, err := client.Discovery().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods").
		Param("labelSelector", selector).DoRaw(ctx)
	if err != nil {
		return PodMetricsList{}, err
	}
	var list PodMetricsList
	if err := json.Unmarshal(raw, &list); err != nil {
		return PodMetricsList{}, err
	}
	return list, nil
}

// Names returns names of pods with a visible metrics sample.
func (l PodMetricsList) Names() []string {
	names := make([]string, 0, len(l.Items))
	for _, item := range l.Items {
		if item.Metadata.Name != "" {
			names = append(names, item.Metadata.Name)
		}
	}
	return names
}
