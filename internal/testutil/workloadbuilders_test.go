package testutil

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildDeployment_DefaultsAndOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		d := BuildDeployment("default", "web")
		if d.Name != "web" || d.Namespace != "default" {
			t.Fatalf("unexpected object meta: %+v", d.ObjectMeta)
		}
		if d.Spec.Replicas == nil || *d.Spec.Replicas != 1 {
			t.Fatalf("expected default replicas=1, got %v", d.Spec.Replicas)
		}
		if len(d.Spec.Template.Spec.Containers) != 0 {
			t.Fatalf("expected no containers by default, got %d", len(d.Spec.Template.Spec.Containers))
		}
	})

	t.Run("with container and status", func(t *testing.T) {
		d := BuildDeployment("default", "web",
			WithContainer(ContainerSpec{
				Name:     "app",
				Requests: map[string]string{string(corev1.ResourceCPU): "100m", string(corev1.ResourceMemory): "128Mi"},
				Limits:   map[string]string{string(corev1.ResourceCPU): "500m"},
			}),
			WithSelector(map[string]string{"app": "web"}),
			WithReplicaStatus(3, 2),
		)
		if d.Spec.Selector == nil || d.Spec.Selector.MatchLabels["app"] != "web" {
			t.Fatalf("expected selector app=web, got %+v", d.Spec.Selector)
		}
		if d.Status.Replicas != 3 || d.Status.ReadyReplicas != 2 {
			t.Fatalf("expected replicas=3 ready=2, got %+v", d.Status)
		}
		cs := d.Spec.Template.Spec.Containers
		if len(cs) != 1 || cs[0].Name != "app" {
			t.Fatalf("expected 1 container 'app', got %+v", cs)
		}
		if got := quantityString(cs[0].Resources.Requests, corev1.ResourceCPU); got != "100m" {
			t.Fatalf("expected cpu request 100m, got %s", got)
		}
		if got := quantityString(cs[0].Resources.Limits, corev1.ResourceCPU); got != "500m" {
			t.Fatalf("expected cpu limit 500m, got %s", got)
		}
		if _, ok := cs[0].Resources.Limits[corev1.ResourceMemory]; ok {
			t.Fatalf("expected no memory limit, got %v", cs[0].Resources.Limits)
		}
	})

	t.Run("default replica count equals 1", func(t *testing.T) {
		d := BuildDeployment("ns", "n")
		if d.Spec.Replicas == nil {
			t.Fatal("expected non-nil replicas pointer")
		}
		if *d.Spec.Replicas != 1 {
			t.Fatalf("expected default replicas 1, got %d", *d.Spec.Replicas)
		}
	})
}

func TestBuildStatefulSet(t *testing.T) {
	sts := BuildStatefulSet("default", "db",
		WithContainer(ContainerSpec{
			Name:     "database",
			Requests: map[string]string{string(corev1.ResourceCPU): "200m"},
		}),
		WithReplicaStatus(5, 4),
	)
	if sts.Spec.ServiceName != "db" {
		t.Fatalf("expected service name 'db', got %q", sts.Spec.ServiceName)
	}
	if sts.Status.Replicas != 5 || sts.Status.ReadyReplicas != 4 {
		t.Fatalf("expected replicas=5 ready=4, got %+v", sts.Status)
	}
	if cs := sts.Spec.Template.Spec.Containers; len(cs) != 1 || cs[0].Name != "database" {
		t.Fatalf("expected container 'database', got %+v", cs)
	}
}

func TestBuildReplicaSet(t *testing.T) {
	rs := BuildReplicaSet("default", "web-abc123",
		WithContainer(ContainerSpec{
			Name:     "app",
			Requests: map[string]string{string(corev1.ResourceCPU): "50m"},
		}),
	)
	cs := rs.Spec.Template.Spec.Containers
	if len(cs) != 1 || cs[0].Name != "app" {
		t.Fatalf("expected container 'app', got %+v", cs)
	}
	if got := quantityString(cs[0].Resources.Requests, corev1.ResourceCPU); got != "50m" {
		t.Fatalf("expected cpu 50m, got %s", got)
	}
}

// quantityString returns the formatted quantity for a resource from a list, or
// "" if absent. resource.Quantity has a pointer-receiver String(), so map index
// access (which copies) cannot call it directly.
func quantityString(list corev1.ResourceList, name corev1.ResourceName) string {
	q, ok := list[name]
	if !ok {
		return ""
	}
	return q.String()
}

func TestBuildPod_SelectorBecomesLabels(t *testing.T) {
	pod := BuildPod("default", "web-abc",
		WithSelector(map[string]string{"app": "web"}),
		WithPodPhase(corev1.PodRunning),
		WithContainerStatus(corev1.ContainerStatus{
			Name:  "app",
			Ready: true,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}),
	)
	if pod.Status.Phase != corev1.PodRunning {
		t.Fatalf("expected Running phase, got %q", pod.Status.Phase)
	}
	if pod.Labels["app"] != "web" {
		t.Fatalf("expected label app=web (from selector), got %v", pod.Labels)
	}
	if len(pod.Status.ContainerStatuses) != 1 || pod.Status.ContainerStatuses[0].Name != "app" {
		t.Fatalf("expected 1 container status 'app', got %+v", pod.Status.ContainerStatuses)
	}
}

func TestBuildPod_PendingUnschedulable(t *testing.T) {
	pod := BuildPod("default", "pod-pending",
		WithSelector(map[string]string{"app": "web"}),
		WithPodPhase(corev1.PodPending),
		WithPodCondition(corev1.PodCondition{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  corev1.PodReasonUnschedulable,
			Message: "Insufficient cpu",
		}),
	)
	if pod.Status.Phase != corev1.PodPending {
		t.Fatalf("expected Pending, got %q", pod.Status.Phase)
	}
	if len(pod.Status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(pod.Status.Conditions))
	}
}

func TestBuildPod_WithPodTemplate(t *testing.T) {
	tmpl := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "custom"}},
		},
	}
	pod := BuildPod("default", "p", WithPodTemplate(tmpl))
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != "custom" {
		t.Fatalf("expected template override, got %+v", pod.Spec.Containers)
	}
}

// TestNewFakeClientWithObjects_AcceptsWorkloads verifies the generalised
// client constructor accepts Deployment/Pod/HPA simultaneously, which is the
// pattern internal/kube and cmd tests need.
func TestNewFakeClientWithObjects_AcceptsWorkloads(t *testing.T) {
	deploy := BuildDeployment("default", "web",
		WithContainer(ContainerSpec{Name: "app"}),
		WithSelector(map[string]string{"app": "web"}),
	)
	pod := BuildPod("default", "web-0",
		WithSelector(map[string]string{"app": "web"}),
		WithPodPhase(corev1.PodRunning),
	)
	hpa := BuildHPA("default", "web")

	client := NewFakeClientWithObjects(deploy, pod, hpa)

	gotDeploy, err := client.AppsV1().Deployments("default").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if gotDeploy.Name != "web" {
		t.Fatalf("expected deploy web, got %q", gotDeploy.Name)
	}

	gotPod, err := client.CoreV1().Pods("default").Get(context.Background(), "web-0", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if gotPod.Status.Phase != corev1.PodRunning {
		t.Fatalf("expected pod Running, got %q", gotPod.Status.Phase)
	}

	var _ = gotDeploy // type assertion smoke test
}

func TestBuildHPA_MetricOptions(t *testing.T) {
	t.Run("pods metric", func(t *testing.T) {
		hpa := BuildHPA("ns", "h", WithPodsMetric("qps", "500", "750"))
		if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].Type != "Pods" {
			t.Fatalf("expected 1 Pods metric, got %+v", hpa.Spec.Metrics)
		}
		if got := hpa.Spec.Metrics[0].Pods.Target.AverageValue.String(); got != "500" {
			t.Fatalf("expected target 500, got %s", got)
		}
		if got := hpa.Status.CurrentMetrics[0].Pods.Current.AverageValue.String(); got != "750" {
			t.Fatalf("expected current 750, got %s", got)
		}
	})

	t.Run("object metric", func(t *testing.T) {
		hpa := BuildHPA("ns", "h", WithObjectMetric("queue-depth", "100", "200"))
		if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].Type != "Object" {
			t.Fatalf("expected 1 Object metric, got %+v", hpa.Spec.Metrics)
		}
		if got := hpa.Spec.Metrics[0].Object.Target.Value.String(); got != "100" {
			t.Fatalf("expected target 100, got %s", got)
		}
	})

	t.Run("container resource metric", func(t *testing.T) {
		hpa := BuildHPA("ns", "h", WithContainerResourceMetric("app", "cpu", 70, 90))
		if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].Type != "ContainerResource" {
			t.Fatalf("expected 1 ContainerResource metric, got %+v", hpa.Spec.Metrics)
		}
		if c := hpa.Spec.Metrics[0].ContainerResource; c == nil || c.Container != "app" {
			t.Fatalf("expected container app, got %+v", c)
		}
	})

	t.Run("multiple metrics accumulate", func(t *testing.T) {
		hpa := BuildHPA("ns", "h",
			WithResourceMetric("cpu", 60, 80),
			WithResourceMetric("memory", 70, 90),
		)
		if len(hpa.Spec.Metrics) != 2 {
			t.Fatalf("expected 2 metrics, got %d", len(hpa.Spec.Metrics))
		}
		if len(hpa.Status.CurrentMetrics) != 2 {
			t.Fatalf("expected 2 current metrics, got %d", len(hpa.Status.CurrentMetrics))
		}
	})
}

func TestBuildHPA_GenerationAndScaleTarget(t *testing.T) {
	hpa := BuildHPA("ns", "h",
		WithGeneration(7),
		WithScaleTargetRef("StatefulSet", "db"),
	)
	if hpa.Generation != 7 {
		t.Fatalf("expected generation 7, got %d", hpa.Generation)
	}
	if hpa.Spec.ScaleTargetRef.Kind != "StatefulSet" || hpa.Spec.ScaleTargetRef.Name != "db" {
		t.Fatalf("unexpected scale target: %+v", hpa.Spec.ScaleTargetRef)
	}
}
