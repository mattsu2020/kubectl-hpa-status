package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
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
	client := fake.NewSimpleClientset(objects...)

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
	client := fake.NewSimpleClientset(objects...)

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
	client := fake.NewSimpleClientset(objects...)

	since := time.Now().Add(-30 * time.Minute)
	result, err := FetchRecentHPAEventsSince(context.Background(), client, namespace, hpaName, since)
	if err != nil {
		t.Fatalf("FetchRecentHPAEventsSince returned error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 events (zero timestamps excluded), got %d", len(result))
	}
}
