package hpa

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAnalyzePods_NilInput(t *testing.T) {
	result := AnalyzePods(nil, nil)
	if result != nil {
		t.Errorf("expected nil for nil pods, got %+v", result)
	}
}

func TestAnalyzePods_EmptyList(t *testing.T) {
	result := AnalyzePods([]corev1.Pod{}, nil)
	if result != nil {
		t.Errorf("expected nil for empty pod list, got %+v", result)
	}
}

func TestAnalyzePods_AllReady(t *testing.T) {
	pods := []corev1.Pod{
		buildPod("pod-1", corev1.PodRunning, true, false),
		buildPod("pod-2", corev1.PodRunning, true, false),
		buildPod("pod-3", corev1.PodRunning, true, false),
	}
	hpa := buildMinimalHPA()

	result := AnalyzePods(pods, hpa)

	if result.Total != 3 {
		t.Errorf("expected Total=3, got %d", result.Total)
	}
	if result.Ready != 3 {
		t.Errorf("expected Ready=3, got %d", result.Ready)
	}
	if result.Unready != 0 {
		t.Errorf("expected Unready=0, got %d", result.Unready)
	}
	if result.Pending != 0 {
		t.Errorf("expected Pending=0, got %d", result.Pending)
	}
}

func TestAnalyzePods_MixedPhases(t *testing.T) {
	pods := []corev1.Pod{
		buildPod("pod-1", corev1.PodRunning, true, false),
		buildPod("pod-2", corev1.PodRunning, false, false),
		buildPod("pod-3", corev1.PodPending, false, false),
		buildPod("pod-4", corev1.PodFailed, false, false),
		buildPod("pod-5", corev1.PodRunning, true, true), // terminating
	}
	hpa := buildMinimalHPA()

	result := AnalyzePods(pods, hpa)

	if result.Total != 5 {
		t.Errorf("expected Total=5, got %d", result.Total)
	}
	if result.Ready != 2 {
		t.Errorf("expected Ready=2, got %d", result.Ready)
	}
	if result.Unready != 2 {
		t.Errorf("expected Unready=2, got %d", result.Unready)
	}
	if result.Pending != 1 {
		t.Errorf("expected Pending=1, got %d", result.Pending)
	}
	if result.Terminating != 1 {
		t.Errorf("expected Terminating=1, got %d", result.Terminating)
	}
}

func TestAnalyzePods_ResourceIssues(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-no-req"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	result := AnalyzePods([]corev1.Pod{pod}, buildMinimalHPA())

	if len(result.ResourceIssues) == 0 {
		t.Fatal("expected missing requests to be detected")
	}

	foundMemory := false
	for _, issue := range result.ResourceIssues {
		if issue.Resource == "memory" && issue.Category == "missing-request" {
			foundMemory = true
		}
	}
	if !foundMemory {
		t.Error("expected missing memory request to be detected")
	}
}

func TestAnalyzePods_ContainerResourceMetricCheck(t *testing.T) {
	pods := []corev1.Pod{
		buildPod("pod-1", corev1.PodRunning, true, false),
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "sidecar",
					},
				},
			},
		},
	}

	result := AnalyzePods(pods, hpa)

	if len(result.ContainerChecks) != 1 {
		t.Fatalf("expected 1 container check, got %d", len(result.ContainerChecks))
	}
	if result.ContainerChecks[0].Found {
		t.Error("expected container 'sidecar' to not be found")
	}
	if result.ContainerChecks[0].Container != "sidecar" {
		t.Errorf("expected container name 'sidecar', got %q", result.ContainerChecks[0].Container)
	}
}

func TestAnalyzePods_ContainerResourceMetricFound(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "sidecar"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: "app",
					},
				},
			},
		},
	}

	result := AnalyzePods([]corev1.Pod{pod}, hpa)

	if len(result.ContainerChecks) != 1 {
		t.Fatalf("expected 1 container check, got %d", len(result.ContainerChecks))
	}
	if !result.ContainerChecks[0].Found {
		t.Error("expected container 'app' to be found")
	}
	if result.ContainerChecks[0].Message != "" {
		t.Errorf("expected empty message for found container, got %q", result.ContainerChecks[0].Message)
	}
}

func buildPod(name string, phase corev1.PodPhase, ready bool, terminating bool) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: phase},
	}

	if ready {
		pod.Status.Conditions = append(pod.Status.Conditions,
			corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue})
	}

	if terminating {
		now := metav1.Now()
		pod.DeletionTimestamp = &now
	}

	return pod
}

func buildMinimalHPA() *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-hpa", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deploy",
			},
			MinReplicas: ptrInt32(1),
			MaxReplicas: 10,
		},
	}
}

func ptrInt32(v int32) *int32 { return &v }
