package kube

import (
	"context"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GetHPA fetches a single HorizontalPodAutoscaler by name from the given
// namespace using the provided interface. Callers wrap the returned error with
// their own user-facing guidance (e.g. actionable hints for NotFound).
func GetHPA(ctx context.Context, iface kubernetes.Interface, namespace, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return iface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		Get(ctx, name, metav1.GetOptions{})
}

// GetHPAFromClient is a convenience wrapper that uses the client's namespace.
func GetHPAFromClient(ctx context.Context, client *Client, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return GetHPA(ctx, client.Interface, client.Namespace, name)
}
