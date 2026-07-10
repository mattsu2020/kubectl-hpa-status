package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

// exitInterrupted follows the shell convention of 128+SIGINT for runs the
// user cancelled; scripts can distinguish it from real failures (1/2).
const exitInterrupted = 130

func main() {
	os.Exit(run())
}

// run executes the root command and returns the process exit code. It exists
// so main can os.Exit after run's deferred signal cleanup has executed.
func run() int {
	// Cancel the root context on SIGINT/SIGTERM so in-flight API calls and
	// watch loops observe ctx.Done() and unwind instead of dying mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := cmd.NewRootCommand().ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		// User-initiated interrupt: not a failure worth an Error: line.
		return exitInterrupted
	}
	// Write errors to stderr are intentionally ignored: we are already on
	// the fatal-error path and cannot recover by reporting a write failure.
	_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return cmd.ExitCodeForMain(err)
}
