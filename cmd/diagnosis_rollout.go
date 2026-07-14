package cmd

import (
	"context"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildRolloutDiagnosis(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.RolloutDiagnosis {
	if client == nil || hpa == nil {
		return nil
	}
	ref := hpa.Spec.ScaleTargetRef
	switch ref.Kind {
	case "Deployment":
		deploy, err := client.Interface.AppsV1().Deployments(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		diag := &hpaanalysis.RolloutDiagnosis{
			Kind:                "Deployment",
			Name:                deploy.Name,
			DesiredReplicas:     replicasOrDefault(deploy.Spec.Replicas),
			UpdatedReplicas:     deploy.Status.UpdatedReplicas,
			ReadyReplicas:       deploy.Status.ReadyReplicas,
			AvailableReplicas:   deploy.Status.AvailableReplicas,
			UnavailableReplicas: deploy.Status.UnavailableReplicas,
		}
		diag.InProgress = deploymentRolloutInProgress(deploy)
		for _, condition := range deploy.Status.Conditions {
			diag.Conditions = append(diag.Conditions, fmt.Sprintf("%s=%s reason=%s", condition.Type, condition.Status, condition.Reason))
		}
		fillRolloutReasonAndPods(ctx, client, hpa, diag)
		return diag
	case "StatefulSet":
		sts, err := client.Interface.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		diag := &hpaanalysis.RolloutDiagnosis{
			Kind:                "StatefulSet",
			Name:                sts.Name,
			DesiredReplicas:     replicasOrDefault(sts.Spec.Replicas),
			UpdatedReplicas:     sts.Status.UpdatedReplicas,
			ReadyReplicas:       sts.Status.ReadyReplicas,
			AvailableReplicas:   sts.Status.AvailableReplicas,
			UnavailableReplicas: sts.Status.Replicas - sts.Status.ReadyReplicas,
			InProgress:          sts.Status.UpdatedReplicas < replicasOrDefault(sts.Spec.Replicas) || sts.Status.ReadyReplicas < replicasOrDefault(sts.Spec.Replicas),
		}
		fillRolloutReasonAndPods(ctx, client, hpa, diag)
		return diag
	default:
		return nil
	}
}

func deploymentRolloutInProgress(deploy *appsv1.Deployment) bool {
	if deploy == nil {
		return false
	}
	desired := replicasOrDefault(deploy.Spec.Replicas)
	return deploy.Status.UpdatedReplicas < desired ||
		deploy.Status.AvailableReplicas < desired ||
		deploy.Status.UnavailableReplicas > 0 ||
		deploy.Generation != deploy.Status.ObservedGeneration
}

func fillRolloutReasonAndPods(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, diag *hpaanalysis.RolloutDiagnosis) {
	if diag == nil {
		return
	}
	if diag.InProgress {
		diag.Reason = "rollout in progress; new pods may not be Ready yet"
		diag.NextActions = append(diag.NextActions, "Inspect rollout status and new pod readiness before changing HPA thresholds.")
	} else {
		diag.Reason = "rollout is not visibly blocking HPA scale-out"
	}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil || info.SelectorStr == "" {
		return
	}
	pods, err := client.Interface.CoreV1().Pods(hpa.Namespace).List(ctx, metav1.ListOptions{LabelSelector: info.SelectorStr})
	if err != nil {
		return
	}
	for _, pod := range pods.Items {
		for _, cs := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
			if cs.State.Waiting == nil {
				continue
			}
			reason := cs.State.Waiting.Reason
			if reason == "ImagePullBackOff" || reason == "ErrImagePull" || reason == "CrashLoopBackOff" {
				diag.PodIssues = append(diag.PodIssues, fmt.Sprintf("%s/%s waiting: %s", pod.Name, cs.Name, reason))
			}
		}
		if pod.Status.Phase == corev1.PodPending {
			diag.PodIssues = append(diag.PodIssues, fmt.Sprintf("%s is Pending", pod.Name))
		}
	}
}
