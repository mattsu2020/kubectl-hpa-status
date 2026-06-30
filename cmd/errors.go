package cmd

import (
	"errors"
	"fmt"
)

// ErrHPANotFound is returned (wrapped) by the status path when the HPA cannot
// be found in the cluster, so callers can match on errors.Is instead of the
// English message text. Defined here rather than in pkg/hpa because the
// not-found case originates from the Kubernetes API client, not the analysis
// model.
var ErrHPANotFound = errors.New("hpa not found")

// ErrNoRecordedSnapshots is returned (wrapped) when a record file contains no
// snapshots for the requested HPA. Both the JSONL and JSON trace loaders wrap
// this sentinel so callers can distinguish "no data" from a parse/IO failure
// via errors.Is rather than substring inspection.
var ErrNoRecordedSnapshots = errors.New("record file has no snapshots for the requested HPA")

// ErrPolicyViolations signals that one or more HPA policy violations were
// detected. Wrapped by the policy lint path so callers can detect the
// "violations found" outcome via errors.Is without matching message text.
var ErrPolicyViolations = errors.New("policy violations found")

// ErrPolicyGuardBlocked signals that the policy guard blocked at least one
// patch in block mode. Wrapped by the apply path so callers can distinguish a
// guard-triggered failure from a generic apply error via errors.Is.
var ErrPolicyGuardBlocked = errors.New("policy guard blocked one or more patches")

// ErrInvalidCandidateSpec signals that a replay/candidate HPA manifest failed
// validation (e.g. non-positive maxReplicas). Wrapped by the candidate loader
// so callers can tell a malformed candidate from an IO/parse failure.
var ErrInvalidCandidateSpec = errors.New("candidate HPA has an invalid spec")

// noSnapshotsError builds the canonical "record file has no snapshots" error
// for the requested namespace/name. Both record loaders route through here so
// the message stays consistent and wraps ErrNoRecordedSnapshots for sentinel
// matching.
func noSnapshotsError(namespace, name string) error {
	if namespace == "" {
		// Match the historical phrasing used by replay_lab when no namespace
		// filter is in play, so existing log/script output is unchanged.
		return fmt.Errorf("record file has no snapshots for namespace %s: %w", namespace, ErrNoRecordedSnapshots)
	}
	return fmt.Errorf("record file has no snapshots for %s/%s: %w", namespace, name, ErrNoRecordedSnapshots)
}
