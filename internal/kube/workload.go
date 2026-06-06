package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ReplicaSetInfo is the observable replica status for a ReplicaSet in the
// HPA scale path.
type ReplicaSetInfo struct {
	Name            string
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// FetchReplicaSetsForScaleTarget returns ReplicaSets that belong to the scale
// target. StatefulSets do not create ReplicaSets, so they return an empty slice.
func FetchReplicaSetsForScaleTarget(ctx context.Context, client kubernetes.Interface, namespace string, ref autoscalingv2.CrossVersionObjectReference, selector string) ([]ReplicaSetInfo, error) {
	switch ref.Kind {
	case "Deployment":
		if selector == "" {
			return nil, nil
		}
		list, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, fmt.Errorf("failed to list ReplicaSets for Deployment %s/%s: %w", namespace, ref.Name, err)
		}
		result := make([]ReplicaSetInfo, 0, len(list.Items))
		for _, rs := range list.Items {
			if !ownedBy(rs.OwnerReferences, "Deployment", ref.Name) {
				continue
			}
			result = append(result, replicaSetInfo(rs))
		}
		return result, nil
	case "ReplicaSet":
		rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get ReplicaSet %s/%s: %w", namespace, ref.Name, err)
		}
		return []ReplicaSetInfo{replicaSetInfo(*rs)}, nil
	case "StatefulSet":
		return nil, nil
	default:
		return nil, nil
	}
}

func replicaSetInfo(rs appsv1.ReplicaSet) ReplicaSetInfo {
	return ReplicaSetInfo{
		Name:            rs.Name,
		DesiredReplicas: replicasValue(rs.Spec.Replicas),
		CurrentReplicas: rs.Status.Replicas,
		ReadyReplicas:   rs.Status.ReadyReplicas,
	}
}

func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, ref := range refs {
		if ref.Kind == kind && ref.Name == name {
			return true
		}
	}
	return false
}
