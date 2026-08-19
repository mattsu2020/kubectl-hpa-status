package kube

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCollectListPagesFollowsContinueToken(t *testing.T) {
	var calls int
	items, err := collectListPages(context.Background(), metav1.ListOptions{}, func(_ context.Context, opts metav1.ListOptions) ([]int, string, error) {
		calls++
		if opts.Limit != defaultListPageSize {
			t.Fatalf("limit = %d", opts.Limit)
		}
		if calls == 1 {
			return []int{1, 2}, "next", nil
		}
		if opts.Continue != "next" {
			t.Fatalf("continue = %q", opts.Continue)
		}
		return []int{3}, "", nil
	})
	if err != nil || len(items) != 3 || calls != 2 {
		t.Fatalf("items=%v calls=%d err=%v", items, calls, err)
	}
}

func TestCollectListPagesPropagatesError(t *testing.T) {
	want := errors.New("list failed")
	_, err := collectListPages(context.Background(), metav1.ListOptions{}, func(context.Context, metav1.ListOptions) ([]int, string, error) {
		return nil, "", want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

// TestCollectListPagesRejectsRepeatedContinueToken verifies the infinite-loop
// guard for servers that keep returning the same continue token.
func TestCollectListPagesRejectsRepeatedContinueToken(t *testing.T) {
	var calls int
	_, err := collectListPages(context.Background(), metav1.ListOptions{}, func(context.Context, metav1.ListOptions) ([]int, string, error) {
		calls++
		return []int{1}, "stuck", nil
	})
	if err == nil {
		t.Fatal("collectListPages() with repeated continue token: want error, got nil")
	}
	if calls > 2 {
		t.Fatalf("list calls = %d, want the guard to abort after the repeated token", calls)
	}
}

// TestCollectListPagesRetriesTransientFailure verifies one page read is
// retried on a transient API error instead of failing the whole walk.
func TestCollectListPagesRetriesTransientFailure(t *testing.T) {
	var calls int
	items, err := collectListPages(context.Background(), metav1.ListOptions{}, func(context.Context, metav1.ListOptions) ([]int, string, error) {
		calls++
		if calls == 1 {
			return nil, "", tooManyRequestsError()
		}
		return []int{7}, "", nil
	})
	if err != nil || len(items) != 1 || calls != 2 {
		t.Fatalf("items=%v calls=%d err=%v", items, calls, err)
	}
}
