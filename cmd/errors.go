package cmd

import (
	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/errs"
)

// This file re-exports the sentinel errors from cmd/internal/errs under the
// unexported names the rest of cmd/ already uses. When the cmd/ sub-package
// split lands, callers should migrate to errs.ErrHPANotFound etc. directly
// and this file can be deleted. The canonical definitions and constructors
// (noSnapshotsError) live in cmd/internal/errs so extracted sub-packages can
// reach them without importing cmd.
//
// Tracking: as of the Phase D refactor (refactor/phase-d-architecture-migration),
// the cmd/ split is still in progress — `cmd/internal/{errs,client,output}`
// and `cmd/bundle` have landed, but the remaining command groups
// (`replay`, `alerts`/`completion`/`compat`/`version`) still live in `cmd/`.
// This shim stays until those groups migrate; do not delete prematurely.

// ErrHPANotFound is returned (wrapped) by the status path when the HPA cannot
// be found in the cluster, so callers can match on errors.Is instead of the
// English message text.
var ErrHPANotFound = errs.ErrHPANotFound

// ErrNoRecordedSnapshots is returned (wrapped) when a record file contains no
// snapshots for the requested HPA.
var ErrNoRecordedSnapshots = errs.ErrNoRecordedSnapshots

// ErrPolicyViolations signals that one or more HPA policy violations were
// detected.
var ErrPolicyViolations = errs.ErrPolicyViolations

// ErrPolicyGuardBlocked signals that the policy guard blocked at least one
// patch in block mode.
var ErrPolicyGuardBlocked = errs.ErrPolicyGuardBlocked

// ErrInvalidCandidateSpec signals that a replay/candidate HPA manifest failed
// validation (e.g. non-positive maxReplicas).
var ErrInvalidCandidateSpec = errs.ErrInvalidCandidateSpec

// noSnapshotsError is the facade for errs.NoSnapshotsError, preserving the
// name call sites already use.
func noSnapshotsError(namespace, name string) error {
	return errs.NoSnapshotsError(namespace, name)
}
