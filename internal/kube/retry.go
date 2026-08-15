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
// with a transient API error, waiting briefly between attempts. The context is
// honored between attempts, so cancellation aborts the loop immediately. The
// last error is returned unchanged (no wrapping) so sentinel matching with
// errors.Is/As keeps working.
func retryTransient[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	var err error
	for attempt := 0; attempt < transientRetryAttempts; attempt++ {
		if attempt > 0 {
			wait := transientRetryBaseWait*time.Duration(attempt) + rand.N(transientRetryJitter) // #nosec G404 -- jitter only decorrelates retry timing; unpredictability is not required
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
