package cmd

import (
	"context"

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
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, namespace, ref)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", nil
	}
	return info.SelectorStr, nil
}
