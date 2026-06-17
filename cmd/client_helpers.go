package cmd

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// wrapClientError formats a Kubernetes client creation error with the standard
// user-facing message used across subcommands. Callers that need a different
// message (or no message) can keep using opts.NewClient() directly.
func wrapClientError(err error) error {
	return fmt.Errorf("failed to create Kubernetes client: %w", err)
}

// newClientOrDefault returns a client or a wrapped error. It is the thin
// convenience form of opts.NewClient() + wrapClientError for commands whose
// only error handling is the standard message.
func newClientOrDefault(opts *options) (*kube.Client, error) {
	client, err := opts.NewClient()
	if err != nil {
		return nil, wrapClientError(err)
	}
	return client, nil
}
