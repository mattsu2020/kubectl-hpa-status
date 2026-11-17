package cmd

import (
	"context"
	"fmt"
	"time"

	hpareadiness "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/readiness"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/metricsapi"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultCPUInitializationPeriod = 5 * time.Minute
)

func buildReadinessImpactWithSnapshot(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, snapshot *observation.Snapshot, now time.Time) *hpareadiness.Impact {
	if client == nil || hpa == nil {
		return nil
	}
	profile := hpaanalysis.DefaultControllerProfile()
	impact := &hpareadiness.Impact{
		InitialReadinessDelay:   profile.InitialReadinessDelay,
		CPUInitializationPeriod: profile.CPUInitializationPeriod,
		NextChecks: []string{
			fmt.Sprintf("kubectl get pod -n %s -l <scale-target-selector>", hpa.Namespace),
			fmt.Sprintf("kubectl top pod -n %s -l <scale-target-selector>", hpa.Namespace),
		},
	}
	if snapshot == nil {
		snapshot = observation.New(client.Interface, hpa)
	}
	target := snapshot.ScaleTarget(ctx)
	if target.State == observation.StateUnavailable {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("scale target selector could not be resolved: %v", target.Err))
		return impact
	}
	if !target.Known() || target.Data.SelectorStr == "" {
		impact.Evidence = append(impact.Evidence, "scale target selector could not be resolved")
		return impact
	}
	info := target.Data
	impact.NextChecks = []string{
		fmt.Sprintf("kubectl get pod -n %s -l %q", hpa.Namespace, info.SelectorStr),
		fmt.Sprintf("kubectl top pod -n %s -l %q", hpa.Namespace, info.SelectorStr),
	}
	pods := snapshot.Pods(ctx)
	if pods.State == observation.StateUnavailable {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("failed to list pods: %v", pods.Err))
		return impact
	}
	if !pods.Known() {
		return impact
	}
	if now.IsZero() {
		now = time.Now()
	}
	impact.TotalPods = int32(len(pods.Data))
	countNotYetReadyPods(impact, pods.Data, hpa.Namespace, now)
	countMissingMetricPods(ctx, impact, client, hpa.Namespace, info.SelectorStr, pods.Data)
	finalizeReadinessImpact(impact)
	return impact
}

// countNotYetReadyPods increments NotYetReadyPods for young non-Ready pods and records evidence and describe-pod next-checks.
func countNotYetReadyPods(impact *hpareadiness.Impact, pods []corev1.Pod, namespace string, now time.Time) {
	for _, pod := range pods {
		if isPodReadyCondition(&pod) {
			continue
		}
		age := time.Duration(0)
		if pod.Status.StartTime != nil {
			age = now.Sub(pod.Status.StartTime.Time).Round(time.Second)
		}
		if age == 0 || age <= defaultCPUInitializationPeriod {
			impact.NotYetReadyPods++
			impact.Evidence = append(impact.Evidence, fmt.Sprintf("pod/%s: Ready=False, age=%s", pod.Name, age))
			if len(impact.NextChecks) < 4 {
				impact.NextChecks = append(impact.NextChecks, fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, namespace))
			}
		}
	}
}

// countMissingMetricPods increments MissingMetricPods for running pods lacking a PodMetrics sample, recording evidence.
func countMissingMetricPods(ctx context.Context, impact *hpareadiness.Impact, client *kube.Client, namespace, selector string, pods []corev1.Pod) {
	missing, metricErr := findMissingMetricPods(ctx, client, namespace, selector, pods)
	if metricErr != nil {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("PodMetrics not checked: %v", metricErr))
		return
	}
	for _, podName := range missing {
		impact.MissingMetricPods++
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("metrics window missing for pod/%s", podName))
	}
}

// finalizeReadinessImpact sets LikelyAffected and appends PossibleEffects based on the recorded counts.
func finalizeReadinessImpact(impact *hpareadiness.Impact) {
	impact.LikelyAffected = impact.NotYetReadyPods > 0 || impact.MissingMetricPods > 0
	if impact.NotYetReadyPods > 0 {
		impact.PossibleEffects = append(impact.PossibleEffects,
			fmt.Sprintf("scale-up may be dampened because %d pod(s) are still initializing", impact.NotYetReadyPods))
	}
	if impact.MissingMetricPods > 0 {
		impact.PossibleEffects = append(impact.PossibleEffects,
			fmt.Sprintf("scale direction may be conservative because %d pod(s) have no visible PodMetrics sample", impact.MissingMetricPods))
	}
	if impact.LikelyAffected {
		impact.PossibleEffects = append(impact.PossibleEffects,
			"HPA status.currentMetrics may not show the adjusted value used internally")
	}
}

func isPodReadyCondition(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// findMissingMetricPods returns the names of non-terminal pods in pods that
// lack a PodMetrics sample.
func findMissingMetricPods(ctx context.Context, client *kube.Client, namespace, selector string, pods []corev1.Pod) ([]string, error) {
	if client == nil || client.Interface == nil || len(pods) == 0 {
		return nil, nil
	}
	list, err := metricsapi.ListPodMetrics(ctx, client.Interface, namespace, selector)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(list.Items))
	for _, name := range list.Names() {
		seen[name] = struct{}{}
	}
	var missing []string
	for i := range pods {
		pod := &pods[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if _, ok := seen[pod.Name]; !ok {
			missing = append(missing, pod.Name)
		}
	}
	return missing, nil
}
