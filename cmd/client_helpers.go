package cmd

import (
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
