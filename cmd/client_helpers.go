package cmd

import (
	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/client"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// This file is a thin facade that re-exports cmd/internal/client under the
// unexported names the rest of cmd/ already uses. When the cmd/ sub-package
// split lands, callers should migrate to client.* directly and this file can
// be deleted. The intentional bypass sites (rollout, blockers, completion,
// autoscaler_map, list, apply) keep using opts.NewClient() directly and are
// documented in ROADMAP.md / ARCHITECTURE.md.
//
// Callers that need a different client-creation error message (or none) can
// keep using opts.NewClient() directly; in that case they should carry a
// comment explaining why the bypass is intentional (best-effort fetch, shell
// completion, structured JSON/YAML error output, applySuggestions
// dual-return).

// wrapHPALookupError re-exports client.WrapHPALookupError.
func wrapHPALookupError(namespace, name string, err error) error {
	return client.WrapHPALookupError(namespace, name, err)
}

// newClientOrDefault re-exports client.NewClientOrDefault.
func newClientOrDefault(opts *options) (*kube.Client, error) {
	return client.NewClientOrDefault(opts)
}
