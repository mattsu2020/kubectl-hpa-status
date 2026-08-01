package cmd

import (
	"context"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

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
