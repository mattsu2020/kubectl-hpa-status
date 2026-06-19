package cmd

import "errors"

// ErrHPANotFound is returned (wrapped) by the status path when the HPA cannot
// be found in the cluster, so callers can match on errors.Is instead of the
// English message text. Defined here rather than in pkg/hpa because the
// not-found case originates from the Kubernetes API client, not the analysis
// model.
var ErrHPANotFound = errors.New("hpa not found")
