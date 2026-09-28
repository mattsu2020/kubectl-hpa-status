package kube

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"time"

	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Retry policy for transient API-server failures (429, 5xx, timeouts).
// A short fixed budget keeps one-shot CLI commands responsive while absorbing
// the brief blips that previously failed a whole report.
const (
	transientRetryAttempts = 3
	transientRetryBaseWait = 200 * time.Millisecond
	transientRetryJitter   = 100 * time.Millisecond
	// transientRetryMaxWait caps a server-provided Retry-After so one hint
	// cannot stall a one-shot CLI command indefinitely.
	transientRetryMaxWait = 5 * time.Second
)

// isTransientAPIError reports whether an API error is worth retrying: server
// side overload (429, 5xx) and network timeouts qualify; client errors such as
// 403/404 and cancelled contexts do not.
func isTransientAPIError(err error) bool {
	if err == nil {
		return false
	}
	if k8sapierrors.IsTooManyRequests(err) ||
		k8sapierrors.IsInternalError(err) ||
		k8sapierrors.IsServiceUnavailable(err) ||
		k8sapierrors.IsServerTimeout(err) ||
		k8sapierrors.IsTimeout(err) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// retryTransient invokes fn up to transientRetryAttempts times while it fails
// with a transient API error, waiting briefly between attempts. A 429's
// Retry-After hint is honored (capped) so a throttling API server is not
// hammered at the fixed cadence. The context is honored between attempts, so
// cancellation aborts the loop immediately. The last error is returned
// unchanged (no wrapping) so sentinel matching with errors.Is/As keeps working.
func retryTransient[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	var err error
	for attempt := 0; attempt < transientRetryAttempts; attempt++ {
		if attempt > 0 {
			wait := retryWait(attempt, err)
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(wait):
			}
		}
		var value T
		value, err = fn()
		if err == nil || !isTransientAPIError(err) {
			return value, err
		}
	}
	return zero, err
}

// retryWait picks the pause before the next attempt: the API server's
// Retry-After when the previous error carries one (capped so a misbehaving
// server cannot stall the CLI), otherwise the fixed backoff with jitter.
func retryWait(attempt int, cause error) time.Duration {
	wait := transientRetryBaseWait*time.Duration(attempt) + rand.N(transientRetryJitter) // #nosec G404 -- jitter only decorrelates retry timing; unpredictability is not required
	if after := retryAfterFrom(cause); after > wait {
		if after > transientRetryMaxWait {
			return transientRetryMaxWait
		}
		return after
	}
	return wait
}

// retryAfterFrom extracts the Retry-After hint from a Kubernetes API error.
func retryAfterFrom(err error) time.Duration {
	var statusErr *k8sapierrors.StatusError
	if errors.As(err, &statusErr) && statusErr.Status().Details != nil {
		if secs := statusErr.Status().Details.RetryAfterSeconds; secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}
