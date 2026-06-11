package cmd

import (
	"github.com/spf13/cobra"
)

func newAdvisorCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "advisor",
		Short: "Run focused HPA configuration advisors",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAdvisorContainerResourceCommand(opts))
	return cmd
}

func newAdvisorContainerResourceCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "container-resource NAME [NAME...]",
		Aliases:           []string{"container", "containerresource"},
		Short:             "Suggest ContainerResource HPA metrics for multi-container workloads",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContainerAdvisor(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}
