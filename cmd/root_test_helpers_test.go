package cmd

// isExitCodeWarning returns true if err is an *ExitCodeError with ExitWarning code.
func isExitCodeWarning(err error) bool {
	exitErr, ok := err.(*ExitCodeError)
	return ok && exitErr.Code == ExitWarning
}
