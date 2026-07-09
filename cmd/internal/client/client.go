// Package client holds the Kubernetes client construction and HPA-lookup
// error-wrapping helpers shared across cmd/ subcommands. Lifted from
// cmd/client_helpers.go so extracted sub-packages can create a client and
// format lookup errors without importing the monolithic cmd package.
package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/errs"
	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// WrapClientError formats a Kubernetes client creation error with the standard
// user-facing message used across subcommands. Callers that need a different
// message (or no message) can keep using opts.NewClient() directly; in that
// case they should carry a comment explaining why the bypass is intentional.
func WrapClientError(err error) error {
	return fmt.Errorf("failed to create Kubernetes client: %w", err)
}

// WrapHPALookupError formats a failed HPA fetch with the canonical
// "failed to get HPA <namespace>/<name>" prefix and attaches ErrHPANotFound so
// every call site (not just the status path) is matchable via errors.Is.
// Passing an empty namespace renders "failed to get HPA <name>" for callers
// that have not yet resolved the namespace. The underlying API error is
// preserved via %w so its status reason is still reachable.
func WrapHPALookupError(namespace, name string, err error) error {
	if namespace == "" {
		return fmt.Errorf("failed to get HPA %s: %w", name, errors.Join(errs.ErrHPANotFound, err))
	}
	return fmt.Errorf("failed to get HPA %s/%s: %w", namespace, name, errors.Join(errs.ErrHPANotFound, err))
}

// NewClientOrDefault returns a client or a wrapped error. It is the thin
// convenience form of opts.NewClient() + WrapClientError for commands whose
// only error handling is the standard message.
func NewClientOrDefault(opts *cmdoptions.Root) (*kube.Client, error) {
	client, err := opts.NewClient()
	if err != nil {
		return nil, WrapClientError(err)
	}
	return client, nil
}

// LookupHPA collapses the two-step pattern used by status-style commands:
// create the standard client, then fetch the named HPA, wrapping a lookup
// failure with the canonical "HPA not found" sentinel so callers can
// errors.Is on it. It uses NewClientOrDefault, so client-creation failures
// carry the standard "failed to create Kubernetes client" message.
//
// Commands that must keep their output schema-clean or treat client failure
// as non-fatal should call opts.NewClient() directly instead.
func LookupHPA(ctx context.Context, opts *cmdoptions.Root, name string) (*kube.Client, *autoscalingv2.HorizontalPodAutoscaler, error) {
	c, err := NewClientOrDefault(opts)
	if err != nil {
		return nil, nil, err
	}
	hpa, err := kube.GetHPAFromClient(ctx, c, name)
	if err != nil {
		return c, nil, WrapHPALookupError(c.Namespace, name, err)
	}
	return c, hpa, nil
}
