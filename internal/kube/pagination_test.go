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
