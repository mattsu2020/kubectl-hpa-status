package cmd

import (
	"errors"
	"fmt"
)

// Exit codes for script integration.
const (
	ExitSuccess = 0 // healthy / success
	ExitError   = 1 // error (API failure)
	ExitWarning = 2 // limited or warning (ScalingActive=False, ScalingLimited, health score below threshold)

	// ExitNotFound is the dedicated code for "the HPA does not exist",
	// letting scripts distinguish it from generic API failures. Applied
	// from v4.0.0; through v3 it exited ExitError for script compatibility.
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
func classifyError(err error) (int, bool) {
	if err == nil {
		return ExitSuccess, true
	}
	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.Code, true
	}
	if errors.Is(err, ErrHPANotFound) {
		return ExitNotFound, true
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
