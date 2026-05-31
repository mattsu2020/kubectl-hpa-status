package cmd

import "fmt"

// Exit codes for script integration.
const (
	ExitSuccess = 0 // healthy / success
	ExitError   = 1 // error (API failure, HPA not found)
	ExitWarning = 2 // limited or warning (ScalingActive=False, ScalingLimited, health score below threshold)
)

// ExitCodeError wraps an error with a specific exit code.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	return e.Err.Error()
}

// warningExitCode returns an ExitCodeError with ExitWarning if the analysis
// health indicates a problem. It returns nil when the health is OK.
// Watch mode (untilCondition is set) always returns nil for success.
func warningExitCode(health, name, namespace string, watchMode bool) error {
	if watchMode {
		return nil
	}
	switch health {
	case "ERROR", "LIMITED":
		return &ExitCodeError{
			Code: ExitWarning,
			Err:  fmt.Errorf("HPA %s/%s health is %s", namespace, name, health),
		}
	}
	return nil
}
