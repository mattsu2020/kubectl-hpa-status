package cmd

import (
	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/client"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// This file is a thin facade that re-exports cmd/internal/client under the
// unexported names the rest of cmd/ already uses. When the cmd/ sub-package
// split lands, callers should migrate to client.* directly and this file can
// be deleted.
//
// Two client-creation idioms coexist by design, not by oversight:
//
//   - newClientOrDefault(opts): the default for commands whose only error
//     handling is the standard "failed to create Kubernetes client" message.
//     Used by ~21 call sites.
//   - opts.NewClient(): the raw form for commands that must bypass the
//     standard wrapper because it would corrupt their output contract or
//     control flow. Each bypass site carries a comment stating the reason.
//     The known intentional bypasses are:
//     - list, autoscaler_map: must surface the raw error so JSON/YAML output
//       stays machine-parseable (an English prefix would break the schema).
//     - blockers, capacity_plan, rollout: client failure is non-fatal; they
//       return an empty result rather than aborting with an error.
//     - completion (x2): shell completion failures stay silent and emit
//       cobra.ShellCompDirectiveNoFileComp.
//     - apply: the caller wraps the error itself for the dual return path.
//
// Do not collapse these to a single idiom without addressing each reason,
// since doing so silently changes user-facing output and exit semantics.

// wrapHPALookupError re-exports client.WrapHPALookupError.
func wrapHPALookupError(namespace, name string, err error) error {
	return client.WrapHPALookupError(namespace, name, err)
}

// newClientOrDefault re-exports client.NewClientOrDefault.
func newClientOrDefault(opts *options) (*kube.Client, error) {
	return client.NewClientOrDefault(opts)
}
