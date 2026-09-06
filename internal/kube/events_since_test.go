package kube

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/testutil"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestFetchRecentHPAEventsSince_TimeFiltering(t *testing.T) {
	now := time.Now()
	namespace := "default"
	hpaName := "web"

	events := []struct {
		reason, message string
		timestamp       time.Time
	}{
		{"SuccessfulRescale", "New size: 5", now.Add(-45 * time.Minute)},
		{"SuccessfulRescale", "New size: 3", now.Add(-15 * time.Minute)},
		{"SuccessfulRescale", "New size: 7", now.Add(-5 * time.Minute)},
	}

	var objects []runtime.Object
	for _, e := range events {
		objects = append(objects, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      hpaName + "." + e.reason + "." + e.timestamp.Format("20060102150405"),
			},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "HorizontalPodAutoscaler",
				Namespace: namespace,
				Name:      hpaName,
			},
			Reason:        e.reason,
			Message:       e.message,
			LastTimestamp: metav1.NewTime(e.timestamp),
		})
	}
	client := testutil.NewFakeClientWithObjects(objects...)

	since := now.Add(-30 * time.Minute)
	result, err := FetchRecentHPAEventsSince(context.Background(), client, namespace, hpaName, since)
	if err != nil {
		t.Fatalf("FetchRecentHPAEventsSince returned error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 events after filtering, got %d", len(result))
	}
	if result[0].Message != "New size: 3" {
		t.Errorf("expected first event 'New size: 3', got %q", result[0].Message)
	}
	if result[1].Message != "New size: 7" {
		t.Errorf("expected second event 'New size: 7', got %q", result[1].Message)
	}
}

func TestFetchRecentHPAEventsForObjectFiltersKindUIDBeforeLimit(t *testing.T) {
	now := time.Now()
	uid := types.UID("current-hpa-uid")
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web", UID: uid},
	}
	objects := []runtime.Object{
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "old"}, InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Name: "web", UID: uid}, Message: "old", LastTimestamp: metav1.NewTime(now.Add(-time.Hour))},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "new"}, InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Name: "web", UID: uid}, Message: "new", LastTimestamp: metav1.NewTime(now)},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "wrong-kind"}, InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "web", UID: uid}, Message: "wrong kind", LastTimestamp: metav1.NewTime(now.Add(time.Minute))},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "wrong-uid"}, InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Name: "web", UID: types.UID("old-hpa-uid")}, Message: "wrong uid", LastTimestamp: metav1.NewTime(now.Add(2 * time.Minute))},
	}
	client := testutil.NewFakeClientWithObjects(objects...)

	result, err := FetchRecentHPAEventsForObject(context.Background(), client, hpa, 1)
	if err != nil {
		t.Fatalf("FetchRecentHPAEventsForObject: %v", err)
	}
	if len(result) != 1 || result[0].Message != "new" {
		t.Fatalf("kind/UID filtering and post-sort limit failed: %+v", result)
	}
}

func TestFetchRecentHPAEventsSince_AscendingOrder(t *testing.T) {
	now := time.Now()
	namespace := "default"
	hpaName := "web"

	objects := []runtime.Object{
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: namespace, Name: "e3"},
			InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Namespace: namespace, Name: hpaName},
			Reason:         "SuccessfulRescale", Message: "New size: 7",
			LastTimestamp: metav1.NewTime(now.Add(-1 * time.Minute)),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: namespace, Name: "e1"},
			InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Namespace: namespace, Name: hpaName},
			Reason:         "SuccessfulRescale", Message: "New size: 3",
			LastTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: namespace, Name: "e2"},
			InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Namespace: namespace, Name: hpaName},
			Reason:         "SuccessfulRescale", Message: "New size: 5",
			LastTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
		},
	}
	client := testutil.NewFakeClientWithObjects(objects...)

	since := now.Add(-15 * time.Minute)
	result, err := FetchRecentHPAEventsSince(context.Background(), client, namespace, hpaName, since)
	if err != nil {
		t.Fatalf("FetchRecentHPAEventsSince returned error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 events, got %d", len(result))
	}
	// Verify ascending order (oldest first).
	for i := 1; i < len(result); i++ {
		tsI := coreEventTimestamp(result[i])
		tsPrev := coreEventTimestamp(result[i-1])
		if tsI.Before(tsPrev) {
			t.Errorf("events not in ascending order: [%d] %v > [%d] %v",
				i-1, tsPrev, i, tsI)
		}
	}
}

func TestFetchRecentHPAEventsSince_ZeroTimestamps(t *testing.T) {
	namespace := "default"
	hpaName := "web"

	objects := []runtime.Object{
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: namespace, Name: "e1"},
			InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Namespace: namespace, Name: hpaName},
			Reason:         "SuccessfulRescale", Message: "New size: 5",
		},
	}
	client := testutil.NewFakeClientWithObjects(objects...)

	since := time.Now().Add(-30 * time.Minute)
	result, err := FetchRecentHPAEventsSince(context.Background(), client, namespace, hpaName, since)
	if err != nil {
		t.Fatalf("FetchRecentHPAEventsSince returned error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 events (zero timestamps excluded), got %d", len(result))
	}
}

// TestFetchRecentHPAEventsSince_NoSilentTruncation pins the fix for the
// silent cap: with more in-window events than the fetch limit, every event in
// the window must come back. The previous implementation truncated to the
// fetch limit BEFORE the time filter, silently dropping the oldest in-window
// events (501 in-window events returned 500 with no indication of loss).
func TestFetchRecentHPAEventsSince_NoSilentTruncation(t *testing.T) {
	now := time.Now()
	namespace := "default"
	hpaName := "web"

	total := eventsSinceFetchLimit + 10
	objects := make([]runtime.Object, 0, total)
	for i := 0; i < total; i++ {
		ts := now.Add(time.Duration(i-total) * time.Minute) // oldest first
		objects = append(objects, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      hpaName + ".SuccessfulRescale." + ts.Format("20060102150405"),
			},
			InvolvedObject: corev1.ObjectReference{Kind: "HorizontalPodAutoscaler", Namespace: namespace, Name: hpaName},
			Reason:         "SuccessfulRescale",
			Message:        "New size: " + strconv.Itoa(i),
			LastTimestamp:  metav1.NewTime(ts),
		})
	}
	client := testutil.NewFakeClientWithObjects(objects...)

	// The window covers every event, including the oldest ones the old
	// fetch-limit-first implementation would have dropped.
	result, err := FetchRecentHPAEventsSince(context.Background(), client, namespace, hpaName, now.Add(-time.Duration(total)*time.Minute))
	if err != nil {
		t.Fatalf("FetchRecentHPAEventsSince returned error: %v", err)
	}
	if len(result) != total {
		t.Fatalf("expected all %d in-window events, got %d", total, len(result))
	}
	first := coreEventTimestamp(result[0])
	if !first.Before(coreEventTimestamp(result[len(result)-1])) {
		t.Fatalf("expected ascending order, got oldest=%v newest=%v", first, coreEventTimestamp(result[len(result)-1]))
	}
	if result[0].Message != "New size: 0" {
		t.Fatalf("expected the oldest event (New size: 0) first, got %q", result[0].Message)
	}
}
