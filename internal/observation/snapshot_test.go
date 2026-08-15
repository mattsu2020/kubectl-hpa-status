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

// TestSnapshotRetriesTransientFailure covers the memoization policy: a failed
// read (e.g. a cancelled context) must not be cached, so a later call with a
// healthy context recovers, while a successful read is still served from the
// cache without a second API call.
func TestSnapshotRetriesTransientFailure(t *testing.T) {
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
	)
	failures := 1
	client.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		if failures > 0 {
			failures--
			return true, nil, errors.New("context deadline exceeded")
		}
		return false, nil, nil
	})
	hpa := testSnapshotHPA()
	snapshot := New(client, &hpa)

	first := snapshot.ScaleTarget(context.Background())
	if first.State != StateUnavailable || first.Err == nil {
		t.Fatalf("first ScaleTarget() = %#v, want unavailable", first)
	}
	second := snapshot.ScaleTarget(context.Background())
	if second.State != StateKnown {
		t.Fatalf("second ScaleTarget() = %#v, want known (failure must not be memoized)", second)
	}
}

// TestSnapshotNilClientUnavailable covers the nil-client guard in ScaleTarget
// and the nil-snapshot guard in Pods.
func TestSnapshotNilClientUnavailable(t *testing.T) {
	// New(nil, nil) yields a snapshot with no client and no HPA.
	snapshot := New(nil, nil)
	target := snapshot.ScaleTarget(context.Background())
	if target.State != StateUnavailable || target.Err == nil {
		t.Fatalf("ScaleTarget() with nil client = %#v", target)
	}

	// A nil snapshot receiver is unavailable as well.
	var nilSnap *Snapshot
	pods := nilSnap.Pods(context.Background())
	if pods.State != StateUnavailable || pods.Err == nil {
		t.Fatalf("nil Snapshot.Pods() = %#v", pods)
	}
}

// TestSnapshotNotApplicableForUnsupportedKind covers the info==nil branch:
// FetchScaleTargetInfo returns (nil, nil) for an unsupported scale target
// kind, which must map to StateNotApplicable rather than an error.
func TestSnapshotNotApplicableForUnsupportedKind(t *testing.T) {
	client := fake.NewClientset()
	hpa := testSnapshotHPA()
	hpa.Spec.ScaleTargetRef = autoscalingv2.CrossVersionObjectReference{Kind: "Job", Name: "batch"}
	snapshot := New(client, &hpa)

	target := snapshot.ScaleTarget(context.Background())
	if target.State != StateNotApplicable {
		t.Fatalf("ScaleTarget() for unsupported kind = %#v, want StateNotApplicable", target)
	}
	pods := snapshot.Pods(context.Background())
	if pods.State != StateNotApplicable {
		t.Fatalf("Pods() for unsupported kind = %#v, want StateNotApplicable", pods)
	}
}
