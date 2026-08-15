package kube

import (
	"context"
	"errors"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func tooManyRequestsError() error {
	return k8sapierrors.NewTooManyRequestsError("rate limited")
}

func notFoundError() error {
	return k8sapierrors.NewNotFound(schema.GroupResource{Group: "autoscaling", Resource: "horizontalpodautoscalers"}, "missing")
}

// TestGetHPARetriesTransientFailure verifies that a 429 is retried and the
// second attempt succeeds.
func TestGetHPARetriesTransientFailure(t *testing.T) {
	client := fake.NewClientset(&autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
	})
	failures := 1
	client.PrependReactor("get", "horizontalpodautoscalers", func(ktesting.Action) (bool, runtime.Object, error) {
		if failures > 0 {
			failures--
			return true, nil, tooManyRequestsError()
		}
		return false, nil, nil
	})

	if _, err := GetHPA(context.Background(), client, "default", "web"); err != nil {
		t.Fatalf("GetHPA() after transient failure: %v", err)
	}
}

// TestGetHPADoesNotRetryClientError verifies that a 404 fails immediately
// without additional attempts.
func TestGetHPADoesNotRetryClientError(t *testing.T) {
	client := fake.NewClientset()
	var gets int
	client.PrependReactor("get", "horizontalpodautoscalers", func(ktesting.Action) (bool, runtime.Object, error) {
		gets++
		return true, nil, notFoundError()
	})

	if _, err := GetHPA(context.Background(), client, "default", "web"); !k8sapierrors.IsNotFound(err) {
		t.Fatalf("GetHPA() error = %v, want not-found", err)
	}
	if gets != 1 {
		t.Fatalf("client calls = %d, want 1 (no retry for 404)", gets)
	}
}

// TestRetryTransientRespectsContext verifies cancellation aborts the retry
// loop between attempts rather than sleeping through it.
func TestRetryTransientRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := retryTransient(ctx, func() (string, error) {
		calls++
		return "", tooManyRequestsError()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retryTransient() error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("fn calls = %d, want 1", calls)
	}
}

// TestListHPAsEachPageRejectsRepeatedContinueToken verifies the infinite-loop
// guard for servers that keep returning the same continue token.
func TestListHPAsEachPageRejectsRepeatedContinueToken(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("list", "horizontalpodautoscalers", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, &autoscalingv2.HorizontalPodAutoscalerList{
			ListMeta: metav1.ListMeta{Continue: "stuck"},
		}, nil
	})

	err := ListHPAsEachPage(context.Background(), client, "default", metav1.ListOptions{}, 1,
		func(*autoscalingv2.HorizontalPodAutoscalerList) error { return nil })
	if err == nil {
		t.Fatal("ListHPAsEachPage() with repeated continue token: want error, got nil")
	}
}
