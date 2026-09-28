package kube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestFetchRecentEventsReportsPartialFailure(t *testing.T) {
	client := fake.NewClientset()
	denied := errors.New("events forbidden")
	client.PrependReactor("list", "events", func(action ktesting.Action) (bool, runtime.Object, error) {
		selector := action.(ktesting.ListAction).GetListRestrictions().Fields.String()
		if strings.Contains(selector, "broken") {
			return true, nil, denied
		}
		return true, &corev1.EventList{Items: []corev1.Event{{InvolvedObject: corev1.ObjectReference{Name: "web"}, Reason: "SuccessfulRescale"}}}, nil
	})
	events, err := FetchRecentEventsForObjects(context.Background(), client, "default", []string{"broken", "web"}, 5)
	if !errors.Is(err, denied) || !strings.Contains(err.Error(), "default/broken") {
		t.Fatalf("missing collection error: %v", err)
	}
	if len(events) != 1 || events[0].Reason != "SuccessfulRescale" {
		t.Fatalf("partial events lost: %+v", events)
	}
}

func TestFetchRecentEventsEmptyIsSuccessful(t *testing.T) {
	events, err := FetchRecentEventsForObjects(context.Background(), fake.NewClientset(), "default", []string{"web"}, 5)
	if err != nil || len(events) != 0 {
		t.Fatalf("events=%v error=%v", events, err)
	}
}

func TestFetchRecentHPAEventsForObjectSinceIdentityAndTime(t *testing.T) {
	now := time.Now()
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web", UID: "current"}}
	var objects []runtime.Object
	for _, item := range []struct {
		name, kind string
		uid        types.UID
		at         time.Time
	}{
		{"before", "HorizontalPodAutoscaler", "current", now.Add(-2 * time.Hour)},
		{"first", "HorizontalPodAutoscaler", "current", now.Add(-time.Hour)},
		{"second", "HorizontalPodAutoscaler", "current", now},
		{"replaced", "HorizontalPodAutoscaler", "old", now},
		{"other-kind", "Deployment", "current", now},
	} {
		objects = append(objects, &corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: item.name}, InvolvedObject: corev1.ObjectReference{Name: "web", Kind: item.kind, UID: item.uid}, LastTimestamp: metav1.NewTime(item.at)})
	}
	client := fake.NewClientset(objects...)
	events, err := FetchRecentHPAEventsForObjectSince(context.Background(), client, hpa, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Name != "first" || events[1].Name != "second" {
		t.Fatalf("wrong history: %+v", events)
	}
	selector := client.Actions()[0].(ktesting.ListAction).GetListRestrictions().Fields
	if got, _ := selector.RequiresExactMatch("involvedObject.uid"); got != "current" {
		t.Fatalf("UID not filtered server-side: %s", selector)
	}
	if _, err := FetchRecentHPAEventsForObjectSince(context.Background(), client, nil, now); err == nil {
		t.Fatal("nil HPA accepted")
	}
}
