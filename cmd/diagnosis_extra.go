package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	defaultCPUInitializationPeriod = 5 * time.Minute
)

func buildReadinessImpact(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.ReadinessImpact {
	if client == nil || hpa == nil {
		return nil
	}
	profile := hpaanalysis.DefaultControllerProfile()
	impact := &hpaanalysis.ReadinessImpact{
		InitialReadinessDelay:   profile.InitialReadinessDelay,
		CPUInitializationPeriod: profile.CPUInitializationPeriod,
		NextChecks: []string{
			fmt.Sprintf("kubectl get pod -n %s -l <scale-target-selector>", hpa.Namespace),
			fmt.Sprintf("kubectl top pod -n %s -l <scale-target-selector>", hpa.Namespace),
		},
	}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil || info.SelectorStr == "" {
		impact.Evidence = append(impact.Evidence, "scale target selector could not be resolved")
		return impact
	}
	impact.NextChecks = []string{
		fmt.Sprintf("kubectl get pod -n %s -l %q", hpa.Namespace, info.SelectorStr),
		fmt.Sprintf("kubectl top pod -n %s -l %q", hpa.Namespace, info.SelectorStr),
	}
	pods, err := client.Interface.CoreV1().Pods(hpa.Namespace).List(ctx, metav1.ListOptions{LabelSelector: info.SelectorStr})
	if err != nil {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("failed to list pods: %v", err))
		return impact
	}
	now := time.Now()
	impact.TotalPods = int32(len(pods.Items))
	countNotYetReadyPods(impact, pods.Items, hpa.Namespace, now)
	countMissingMetricPods(ctx, impact, client, hpa.Namespace, info.SelectorStr, pods.Items)
	finalizeReadinessImpact(impact)
	return impact
}

// countNotYetReadyPods increments NotYetReadyPods for young non-Ready pods and records evidence and describe-pod next-checks.
func countNotYetReadyPods(impact *hpaanalysis.ReadinessImpact, pods []corev1.Pod, namespace string, now time.Time) {
	for _, pod := range pods {
		if podReadyForImpact(pod) {
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
func countMissingMetricPods(ctx context.Context, impact *hpaanalysis.ReadinessImpact, client *kube.Client, namespace, selector string, pods []corev1.Pod) {
	metricPods, metricErr := fetchPodMetricNames(ctx, client, namespace, selector)
	if metricErr != nil {
		impact.Evidence = append(impact.Evidence, fmt.Sprintf("PodMetrics not checked: %v", metricErr))
		return
	}
	seen := make(map[string]struct{}, len(metricPods))
	for _, name := range metricPods {
		seen[name] = struct{}{}
	}
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if _, ok := seen[pod.Name]; !ok {
			impact.MissingMetricPods++
			impact.Evidence = append(impact.Evidence, fmt.Sprintf("metrics window missing for pod/%s", pod.Name))
		}
	}
}

// finalizeReadinessImpact sets LikelyAffected and appends PossibleEffects based on the recorded counts.
func finalizeReadinessImpact(impact *hpaanalysis.ReadinessImpact) {
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

func podReadyForImpact(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

type podMetricsNamesJSON struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"items"`
}

func fetchPodMetricNames(ctx context.Context, client *kube.Client, namespace, selector string) ([]string, error) {
	restClient := client.Interface.Discovery().RESTClient()
	if restClient == nil {
		return nil, fmt.Errorf("discovery REST client is unavailable")
	}
	raw, err := restClient.Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods").
		Param("labelSelector", selector).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var list podMetricsNamesJSON
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Metadata.Name != "" {
			names = append(names, item.Metadata.Name)
		}
	}
	return names, nil
}

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

func buildControllerProfile(ctx context.Context, client *kube.Client, opts *options) *hpaanalysis.ControllerProfile {
	profile := hpaanalysis.DefaultControllerProfile()
	if opts != nil && opts.assumeProfile != "" {
		profile.Source = "assumed:" + opts.assumeProfile
		return &profile
	}
	if opts != nil && opts.controllerProfileFile != "" {
		loaded, err := loadControllerProfileFile(opts.controllerProfileFile)
		if err == nil {
			return loaded
		}
		profile.Warnings = append(profile.Warnings, fmt.Sprintf("failed to load controller profile file: %v", err))
	}
	if client == nil {
		return &profile
	}
	observed, ok := observeControllerManagerProfile(ctx, client)
	if !ok {
		profile.Warnings = append(profile.Warnings, "kube-controller-manager args were not visible; using Kubernetes defaults")
		return &profile
	}
	return observed
}

func loadControllerProfileFile(path string) (*hpaanalysis.ControllerProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	profile := hpaanalysis.DefaultControllerProfile()
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, err
	}
	if profile.Source == "" {
		profile.Source = "file:" + path
	}
	return &profile, nil
}

func observeControllerManagerProfile(ctx context.Context, client *kube.Client) (*hpaanalysis.ControllerProfile, bool) {
	pods, err := client.Interface.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, false
	}
	for _, pod := range pods.Items {
		if !strings.Contains(pod.Name, "kube-controller-manager") {
			continue
		}
		profile := hpaanalysis.DefaultControllerProfile()
		profile.Source = "kube-system/" + pod.Name
		for _, container := range pod.Spec.Containers {
			for _, arg := range container.Command {
				applyControllerArg(&profile, arg)
			}
			for _, arg := range container.Args {
				applyControllerArg(&profile, arg)
			}
		}
		return &profile, true
	}
	return nil, false
}

func applyControllerArg(profile *hpaanalysis.ControllerProfile, arg string) {
	if profile == nil || !strings.HasPrefix(arg, "--") {
		return
	}
	key, value, ok := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	if !ok {
		return
	}
	switch key {
	case "horizontal-pod-autoscaler-sync-period":
		profile.SyncPeriod = value
	case "horizontal-pod-autoscaler-downscale-stabilization":
		profile.DownscaleStabilization = value
	case "horizontal-pod-autoscaler-initial-readiness-delay":
		profile.InitialReadinessDelay = value
	case "horizontal-pod-autoscaler-cpu-initialization-period":
		profile.CPUInitializationPeriod = value
	case "horizontal-pod-autoscaler-tolerance":
		profile.Tolerance = value
	}
}

func buildCapacityHeadroom(ctx context.Context, client *kube.Client, hpa *autoscalingv2.HorizontalPodAutoscaler, target string) *hpaanalysis.CapacityHeadroom {
	if client == nil || hpa == nil {
		return nil
	}
	info, err := kube.FetchScaleTargetInfo(ctx, client.Interface, hpa.Namespace, hpa.Spec.ScaleTargetRef)
	if err != nil || info == nil || info.PodTemplate == nil {
		return nil
	}
	cpuPerPod, memPerPod := sumPodTemplateRequests(info.PodTemplate)
	additional := hpa.Spec.MaxReplicas - hpa.Status.DesiredReplicas
	if additional < 0 {
		additional = 0
	}
	addCPU := multiplyQuantity(cpuPerPod, additional)
	addMem := multiplyQuantity(memPerPod, additional)
	headroom := &hpaanalysis.CapacityHeadroom{
		HPAName:                    hpa.Name,
		Target:                     target,
		MaxReplicas:                hpa.Spec.MaxReplicas,
		CurrentDesired:             hpa.Status.DesiredReplicas,
		AdditionalReplicasToMax:    additional,
		PodRequestCPU:              quantityOrEmpty(cpuPerPod),
		PodRequestMemory:           quantityOrEmpty(memPerPod),
		AdditionalCPUToMax:         quantityOrEmpty(addCPU),
		AdditionalMemoryToMax:      quantityOrEmpty(addMem),
		ClusterSchedulableHeadroom: "unknown",
		Risk:                       "cluster schedulable headroom could not be confirmed from visible API data",
	}
	nodeCap, nodeErr := kube.FetchNodeCapacity(ctx, client.Interface)
	usedCPU, usedMem := sumScheduledPodRequests(ctx, client)
	if nodeErr == nil && nodeCap != nil {
		cpuRemaining := nodeCap.AllocCPU.DeepCopy()
		cpuRemaining.Sub(usedCPU)
		memRemaining := nodeCap.AllocMemory.DeepCopy()
		memRemaining.Sub(usedMem)
		headroom.Evidence = append(headroom.Evidence,
			fmt.Sprintf("nodes=%d allocatable cpu=%s memory=%s", nodeCap.TotalNodes, nodeCap.AllocCPU.String(), nodeCap.AllocMemory.String()),
			fmt.Sprintf("scheduled pod requests cpu=%s memory=%s", usedCPU.String(), usedMem.String()),
			fmt.Sprintf("remaining request headroom cpu=%s memory=%s", cpuRemaining.String(), memRemaining.String()),
		)
		switch {
		case additional == 0:
			headroom.ClusterSchedulableHeadroom = "none needed"
			headroom.Risk = "HPA desiredReplicas is already at or above maxReplicas"
		case quantityAtLeast(cpuRemaining, addCPU) && quantityAtLeast(memRemaining, addMem):
			headroom.ClusterSchedulableHeadroom = "available"
			headroom.Risk = "visible node allocatable request headroom appears sufficient; scheduler constraints may still apply"
		default:
			headroom.ClusterSchedulableHeadroom = "low"
			headroom.Risk = "HPA can request more Pods, but Pods may stay Pending"
		}
	}
	return headroom
}

func sumPodTemplateRequests(tmpl *corev1.PodTemplateSpec) (resource.Quantity, resource.Quantity) {
	var cpu, mem resource.Quantity
	if tmpl == nil {
		return cpu, mem
	}
	for _, container := range tmpl.Spec.Containers {
		if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			cpu.Add(q)
		}
		if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			mem.Add(q)
		}
	}
	return cpu, mem
}

func sumScheduledPodRequests(ctx context.Context, client *kube.Client) (resource.Quantity, resource.Quantity) {
	var cpu, mem resource.Quantity
	pods, err := client.Interface.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return cpu, mem
	}
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, container := range pod.Spec.Containers {
			if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				cpu.Add(q)
			}
			if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				mem.Add(q)
			}
		}
	}
	return cpu, mem
}

func multiplyQuantity(q resource.Quantity, factor int32) resource.Quantity {
	out := q.DeepCopy()
	if factor <= 0 || q.IsZero() {
		return resource.Quantity{}
	}
	out.SetMilli(q.MilliValue() * int64(factor))
	return out
}

func quantityOrEmpty(q resource.Quantity) string {
	if q.IsZero() {
		return ""
	}
	return q.String()
}

func quantityAtLeast(have, need resource.Quantity) bool {
	return need.IsZero() || have.Cmp(need) >= 0
}
