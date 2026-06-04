package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fetchAndAnalyzePods retrieves pods for the HPA scale target and runs pod-level analysis.
func fetchAndAnalyzePods(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.PodAnalysis {
	ref := hpa.Spec.ScaleTargetRef
	selector, err := resolveScaleTargetSelector(ctx, client, hpa.Namespace, ref)
	if err != nil {
		return &hpaanalysis.PodAnalysis{
			Total: 0,
		}
	}
	if selector == "" {
		return nil
	}

	pods, err := client.Interface.CoreV1().Pods(hpa.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil
	}

	return hpaanalysis.AnalyzePods(pods.Items, hpa)
}

// resolveScaleTargetSelector returns the label selector string for the HPA scale target.
func resolveScaleTargetSelector(ctx context.Context, client *kube.Client, namespace string, ref autoscalingv2.CrossVersionObjectReference) (string, error) {
	switch ref.Kind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get deployment %s: %w", ref.Name, err)
		}
		return metav1.FormatLabelSelector(deploy.Spec.Selector), nil
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get statefulset %s: %w", ref.Name, err)
		}
		return metav1.FormatLabelSelector(sts.Spec.Selector), nil
	case "ReplicaSet":
		rs, err := client.Interface.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get replicaset %s: %w", ref.Name, err)
		}
		if rs.Spec.Selector != nil {
			return metav1.FormatLabelSelector(rs.Spec.Selector), nil
		}
		return "", nil
	default:
		return "", nil
	}
}
