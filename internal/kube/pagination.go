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
	if opts.Limit <= 0 {
		opts.Limit = defaultListPageSize
	}
	opts.Continue = ""
	var all []T
	for {
		items, next, err := list(ctx, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		opts.Continue = next
	}
}
