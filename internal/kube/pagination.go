package kube

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultListPageSize int64 = 500

// listPage pairs one page's items with the continue token leading to the next
// page so the transient-retry helper can treat them as a single value.
type listPage[T any] struct {
	items []T
	next  string
}

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
// this form so their peak memory is bounded by the Kubernetes page size. Every
// page read is retried briefly on transient API-server failures (429/5xx/
// timeouts), and a server that keeps returning the same continue token aborts
// the walk instead of looping forever.
func visitListPages[T any](ctx context.Context, opts metav1.ListOptions, list func(context.Context, metav1.ListOptions) ([]T, string, error), visit func([]T) error) error {
	if opts.Limit <= 0 {
		opts.Limit = defaultListPageSize
	}
	opts.Continue = ""
	seenContinue := make(map[string]bool)
	for {
		page, err := retryTransient(ctx, func() (listPage[T], error) {
			items, next, err := list(ctx, opts)
			return listPage[T]{items: items, next: next}, err
		})
		if err != nil {
			return err
		}
		if err := visit(page.items); err != nil {
			return err
		}
		if page.next == "" {
			return nil
		}
		if err := guardRepeatedContinueToken(seenContinue, page.next, "paginated list"); err != nil {
			return err
		}
		opts.Continue = page.next
	}
}

// guardRepeatedContinueToken records next and reports an error when it was
// already followed, so every pagination loop shares one defense against
// servers that hand back the same continue token forever.
func guardRepeatedContinueToken(seen map[string]bool, next, what string) error {
	if seen[next] {
		return fmt.Errorf("%s: server returned a repeated continue token", what)
	}
	seen[next] = true
	return nil
}
