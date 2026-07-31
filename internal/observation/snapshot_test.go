package observation

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestSnapshotMemoizesTargetAndPodsAcrossDerivedViews(t *testing.T) {
	replicas := int32(1)
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	client := fake.NewClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: selector,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web-1", Labels: map[string]string{"app": "web"}},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  "app",
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
				}},
			},
		},
	)
	hpa := testSnapshotHPA()
	snapshot := New(client, &hpa)

	if got := snapshot.PodInfos(context.Background()); !got.Known() || len(got.Data) != 1 {
		t.Fatalf("PodInfos() = %#v", got)
	}
	if got := snapshot.PendingPods(context.Background()); !got.Known() || len(got.Data) != 1 {
		t.Fatalf("PendingPods() = %#v", got)
	}
	if got := snapshot.ContainerStatuses(context.Background()); !got.Known() || len(got.Data) != 1 {
		t.Fatalf("ContainerStatuses() = %#v", got)
	}

	var deploymentGets, podLists int
	for _, action := range client.Actions() {
		switch {
		case action.Matches("get", "deployments"):
			deploymentGets++
		case action.Matches("list", "pods"):
			podLists++
		}
	}
	if deploymentGets != 1 || podLists != 1 {
		t.Fatalf("API calls: deployment gets=%d, pod lists=%d", deploymentGets, podLists)
	}
}

func TestSnapshotPreservesUnavailableState(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	hpa := testSnapshotHPA()
	snapshot := New(client, &hpa)

	target := snapshot.ScaleTarget(context.Background())
	if target.State != StateUnavailable || target.Err == nil {
		t.Fatalf("ScaleTarget() = %#v", target)
	}
	pods := snapshot.Pods(context.Background())
	if pods.State != StateUnavailable || pods.Err == nil {
		t.Fatalf("Pods() = %#v", pods)
	}
}

func testSnapshotHPA() autoscalingv2.HorizontalPodAutoscaler {
	return autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			MaxReplicas:    10,
		},
	}
}
