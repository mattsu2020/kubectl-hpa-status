package main

import (
	"os"

	"github.com/mattsu2020/kubehpa_cli/cmd"
)

func main() {
	if err := cmd.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
