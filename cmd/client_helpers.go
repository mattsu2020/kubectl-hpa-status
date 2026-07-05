package cmd

import (
	"context"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/client"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
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

// lookupHPA collapses the two-step pattern repeated across ~20 status-style
// commands: create the standard client, then fetch the named HPA, wrapping a
// lookup failure with the canonical "HPA not found" sentinel so callers can
// errors.Is on it. It uses newClientOrDefault, so it carries the standard
// "failed to create Kubernetes client" message on client-creation failure
// (no --no-wrap bypass). Commands that must keep their output schema-clean
// or treat client failure as non-fatal should keep calling opts.NewClient()
// directly — see the bypass list in the file comment above.
func lookupHPA(ctx context.Context, opts *options, name string) (*kube.Client, *autoscalingv2.HorizontalPodAutoscaler, error) {
	c, err := newClientOrDefault(opts)
	if err != nil {
		return nil, nil, err
	}
	hpa, err := kube.GetHPAFromClient(ctx, c, name)
	if err != nil {
		return c, nil, wrapHPALookupError(c.Namespace, name, err)
	}
	return c, hpa, nil
}
