package kube

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// FetchPodsForScaleTarget resolves the scale target's label selector and lists
// all pods matching it. Returns the pod list or an error if the scale target
// kind is unsupported or the selector cannot be resolved.
func FetchPodsForScaleTarget(ctx context.Context, client kubernetes.Interface, namespace string, hpa *autoscalingv2.HorizontalPodAutoscaler) ([]string, error) {
	ref := hpa.Spec.ScaleTargetRef
	if ref.Kind != "Deployment" && ref.Kind != "StatefulSet" && ref.Kind != "ReplicaSet" {
		return nil, fmt.Errorf("unsupported scale target kind %q", ref.Kind)
	}

	selector, err := resolveLabelSelector(ctx, client, namespace, ref)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve label selector for %s/%s: %w", ref.Kind, ref.Name, err)
	}
	if selector == "" {
		return nil, nil
	}

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	names := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		names = append(names, pod.Name)
	}
	return names, nil
}

// resolveLabelSelector returns the label selector string for the given scale target reference.
func resolveLabelSelector(ctx context.Context, client kubernetes.Interface, namespace string, ref autoscalingv2.CrossVersionObjectReference) (string, error) {
	switch ref.Kind {
	case "Deployment":
		deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return metav1.FormatLabelSelector(deploy.Spec.Selector), nil
	case "StatefulSet":
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return metav1.FormatLabelSelector(sts.Spec.Selector), nil
	case "ReplicaSet":
		rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if rs.Spec.Selector != nil {
			return metav1.FormatLabelSelector(rs.Spec.Selector), nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("unsupported scale target kind %q", ref.Kind)
	}
}
