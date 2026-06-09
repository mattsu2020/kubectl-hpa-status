package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGitOpsCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitops --path PATH",
		Short: "Lint GitOps manifests offline for HPA conflicts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("path")
			outputFmt, _ := cmd.Flags().GetString("output")
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			return runLint(cmd.Context(), cmd.OutOrStdout(), opts, path, outputFmt, false, false)
		},
	}
	cmd.Flags().String("path", "", "path to manifest file or directory")
	cmd.Flags().StringP("output", "o", "", "output format: text, json, sarif")
	return cmd
}
