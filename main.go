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
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
