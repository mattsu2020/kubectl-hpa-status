package kube

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultListPageSize int64 = 500

// collectListPages implements the Kubernetes continue-token contract for
// typed and dynamic clients. Callers only provide the resource-specific List
// adapter, keeping pagination behavior consistent across collectors.
func collectListPages[T any](ctx context.Context, opts metav1.ListOptions, list func(context.Context, metav1.ListOptions) ([]T, string, error)) ([]T, error) {
	var all []T
	err := visitListPages(ctx, opts, list, func(items []T) error {
		all = append(all, items...)
		return nil
	})
	return all, err
}

// visitListPages processes one API page at a time. Aggregations should prefer
// this form so their peak memory is bounded by the Kubernetes page size.
func visitListPages[T any](ctx context.Context, opts metav1.ListOptions, list func(context.Context, metav1.ListOptions) ([]T, string, error), visit func([]T) error) error {
	if opts.Limit <= 0 {
		opts.Limit = defaultListPageSize
	}
	opts.Continue = ""
	for {
		items, next, err := list(ctx, opts)
		if err != nil {
			return err
		}
		if err := visit(items); err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		opts.Continue = next
	}
}
