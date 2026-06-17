package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
)

func TestRunPathShowsSchedulerBlocker(t *testing.T) {
	replicas := int32(12)
	hpa := testutil.BuildHPA("default", "web", testutil.WithReplicas(8, 12))
	deployUID := types.UID("deploy-web")
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web",
			UID:       deployUID,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
		Status: appsv1.DeploymentStatus{Replicas: 12, ReadyReplicas: 8},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-abc",
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "Deployment",
				Name: "web",
				UID:  deployUID,
			}},
			Labels: map[string]string{"app": "web"},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
		Status: appsv1.ReplicaSetStatus{Replicas: 12, ReadyReplicas: 8},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "web-pending",
			Labels:    map[string]string{"app": "web"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionFalse,
				Reason:  corev1.PodReasonUnschedulable,
				Message: "0/5 nodes available: insufficient cpu",
			}},
		},
	}
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sched"},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "default",
			Name:      "web-pending",
		},
		Reason:        "FailedScheduling",
		Message:       "0/5 nodes available: insufficient cpu",
		LastTimestamp: metav1.Now(),
	}
	client := fake.NewClientset(hpa, deploy, rs, pod, event)
	opts := &options{
		Common: cmdoptions.Common{
			ClientOverride: client,
			Namespace:      "default",
		},
		Status: cmdoptions.Status{
			Events: EventOption{Enabled: false},
		},
	}

	var out bytes.Buffer
	if err := runPath(context.Background(), &out, opts, []string{"web"}); err != nil {
		t.Fatalf("runPath returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Scale Path:") {
		t.Fatalf("expected Scale Path output, got:\n%s", got)
	}
	if !strings.Contains(got, "Scheduler cannot place 1 pods") {
		t.Fatalf("expected scheduler blocker, got:\n%s", got)
	}
	if !strings.Contains(got, "maxReplicas is not the current blocker") {
		t.Fatalf("expected maxReplicas evidence, got:\n%s", got)
	}
}
