package cmd

import (
	"errors"
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// wrapClientError formats a Kubernetes client creation error with the standard
// user-facing message used across subcommands. Callers that need a different
// message (or no message) can keep using opts.NewClient() directly; in that
// case they should carry a comment explaining why the bypass is intentional
// (best-effort fetch, shell completion, structured JSON/YAML error output,
// applySuggestions dual-return). The intentional bypass sites today are:
//   - rollout.go, blockers.go, capacity_plan.go (best-effort nil on failure)
//   - completion.go hpaNameCompletion + namespaceCompletions (silent shell comp)
//   - autoscaler_map.go, list.go (structured JSON/YAML error documents)
//   - apply.go applySuggestions (dual messages+err return contract)
func wrapClientError(err error) error {
	return fmt.Errorf("failed to create Kubernetes client: %w", err)
}

// wrapHPALookupError formats a failed HPA fetch with the canonical
// "failed to get HPA <namespace>/<name>" prefix and attaches ErrHPANotFound so
// every call site (not just the status path) is matchable via errors.Is.
// Passing an empty namespace renders "failed to get HPA <name>" for callers
// that have not yet resolved the namespace (e.g. commands reading a bare name
// from a flag before the client is built). The underlying API error (typically
// a NotFound from apierrors) is preserved via %w so its status reason is still
// reachable.
func wrapHPALookupError(namespace, name string, err error) error {
	if namespace == "" {
		return fmt.Errorf("failed to get HPA %s: %w", name, errors.Join(ErrHPANotFound, err))
	}
	return fmt.Errorf("failed to get HPA %s/%s: %w", namespace, name, errors.Join(ErrHPANotFound, err))
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
