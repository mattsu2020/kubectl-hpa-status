package cmd

import (
	"errors"
	"fmt"
)

// Exit codes for script integration.
const (
	ExitSuccess = 0 // healthy / success
	ExitError   = 1 // error (API failure, HPA not found)
	ExitWarning = 2 // limited or warning (ScalingActive=False, ScalingLimited, health score below threshold)

	// ExitNotFound is reserved for a future release where HPA-not-found exits
	// with a dedicated code distinct from generic API errors. The current
	// behavior keeps not-found at ExitError for backwards compatibility; the
	// constant is exported so scripts and docs can adopt it incrementally.
	// Application is gated behind the v4.0.0 boundary (see ROADMAP.md).
	ExitNotFound = 3
)

// ExitCodeError wraps an error with a specific exit code.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	return e.Err.Error()
}

// Unwrap allows errors.Is/As to reach the wrapped cause, so callers that match
// on a sentinel (e.g. ErrHPANotFound) still resolve it through an
// ExitCodeError wrapper.
func (e *ExitCodeError) Unwrap() error { return e.Err }

// warningExitCode returns an ExitCodeError with ExitWarning if the analysis
// health indicates a problem. It returns nil when the health is OK.
// Watch mode (untilCondition is set) always returns nil for success.
// The accepted states match healthIsWarning (ERROR / LIMITED / WARNING) so a
// single-HPA status and any member of a batch status produce the same exit
// code for the same health.
func warningExitCode(health, name, namespace string, watchMode bool) error {
	if watchMode {
		return nil
	}
	if healthIsWarning(health) {
		return &ExitCodeError{
			Code: ExitWarning,
			Err:  fmt.Errorf("HPA %s/%s health is %s", namespace, name, health),
		}
	}
	return nil
}

// classifyError maps a returned error to a concrete exit code using sentinel
// matching rather than substring inspection. It returns (code, true) when the
// error is recognised, or (ExitError, false) as the generic fallback. This is
// the single place where sentinel -> exit-code mapping lives so new sentinels
// are added here instead of leaking ad-hoc switches into command Run handlers.
//
// Currently every sentinel still resolves to ExitError to preserve existing
// script behavior; the function exists so the mapping can be tightened in a
// single place when the dedicated exit codes roll out.
func classifyError(err error) (int, bool) {
	if err == nil {
		return ExitSuccess, true
	}
	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.Code, true
	}
	if errors.Is(err, ErrHPANotFound) {
		// Kept at ExitError for backwards compatibility; flip to ExitNotFound
		// at the v4.0.0 boundary tracked in ROADMAP.md.
		return ExitError, true
	}
	return ExitError, false
}

// exitCodeForError returns the exit code an error should produce. It is a
// thin convenience over classifyError for callers that only need the code.
func exitCodeForError(err error) int {
	code, _ := classifyError(err)
	return code
}

// ExitCodeForMain resolves the process exit code for a top-level command
// error. It is the single entry point main() uses, centralising the
// ExitCodeError -> sentinel -> generic fallback chain so command handlers do
// not each reimplement the dispatch.
func ExitCodeForMain(err error) int {
	if err == nil {
		return ExitSuccess
	}
	return exitCodeForError(err)
}
