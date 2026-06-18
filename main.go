package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

func main() {
	if err := cmd.NewRootCommand().Execute(); err != nil {
		var exitErr *cmd.ExitCodeError
		if errors.As(err, &exitErr) {
			// Write errors to stderr are intentionally ignored: we are already on
			// the fatal-error path and cannot recover by reporting a write failure.
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitErr.Code)
		}
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
