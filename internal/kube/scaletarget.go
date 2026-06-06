package kube

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ScaleTargetInfo holds the resolved information about an HPA's scale target.
type ScaleTargetInfo struct {
	Kind            string
	Name            string
	Namespace       string
	Selector        *metav1.LabelSelector
	SelectorStr     string
	DesiredReplicas int32
	Replicas        int32
	ReadyReplicas   int32
	PodTemplate     *corev1.PodTemplateSpec
}

// FetchScaleTargetInfo resolves the HPA scale target reference into a
// ScaleTargetInfo containing the label selector, replica counts, and pod template.
// Returns nil for unsupported kinds without error.
func FetchScaleTargetInfo(ctx context.Context, client kubernetes.Interface, namespace string, ref autoscalingv2.CrossVersionObjectReference) (*ScaleTargetInfo, error) {
	switch ref.Kind {
	case "Deployment":
		deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get Deployment %s/%s: %w", namespace, ref.Name, err)
		}
		return &ScaleTargetInfo{
			Kind:            ref.Kind,
			Name:            ref.Name,
			Namespace:       namespace,
			Selector:        deploy.Spec.Selector,
			SelectorStr:     metav1.FormatLabelSelector(deploy.Spec.Selector),
			DesiredReplicas: replicasValue(deploy.Spec.Replicas),
			Replicas:        deploy.Status.Replicas,
			ReadyReplicas:   deploy.Status.ReadyReplicas,
			PodTemplate:     &deploy.Spec.Template,
		}, nil
	case "StatefulSet":
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get StatefulSet %s/%s: %w", namespace, ref.Name, err)
		}
		return &ScaleTargetInfo{
			Kind:            ref.Kind,
			Name:            ref.Name,
			Namespace:       namespace,
			Selector:        sts.Spec.Selector,
			SelectorStr:     metav1.FormatLabelSelector(sts.Spec.Selector),
			DesiredReplicas: replicasValue(sts.Spec.Replicas),
			Replicas:        sts.Status.Replicas,
			ReadyReplicas:   sts.Status.ReadyReplicas,
			PodTemplate:     &sts.Spec.Template,
		}, nil
	case "ReplicaSet":
		rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get ReplicaSet %s/%s: %w", namespace, ref.Name, err)
		}
		return &ScaleTargetInfo{
			Kind:            ref.Kind,
			Name:            ref.Name,
			Namespace:       namespace,
			Selector:        rs.Spec.Selector,
			SelectorStr:     metav1.FormatLabelSelector(rs.Spec.Selector),
			DesiredReplicas: replicasValue(rs.Spec.Replicas),
			Replicas:        rs.Status.Replicas,
			ReadyReplicas:   rs.Status.ReadyReplicas,
			PodTemplate:     &rs.Spec.Template,
		}, nil
	default:
		return nil, nil
	}
}

func replicasValue(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}
