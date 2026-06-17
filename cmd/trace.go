package cmd

import (
	"context"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

func newTraceCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "trace NAME [NAME...]",
		Short:             "Show a step-by-step HPA decision trace",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrace(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runTrace(ctx context.Context, out io.Writer, opts *options, names []string) error {
	// Enable decision-trace collection. Take a shallow copy so the shared
	// process-wide opts is not mutated.
	local := copyOptions(opts)
	local.features.decisionTrace = true
	reports := make([]hpaanalysis.StatusReport, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReportWithClient(ctx, &local, name, false, nil)
		if err != nil {
			return err
		}
		reports = append(reports, report)
	}
	for i, report := range reports {
		if i > 0 {
			if _, err := io.WriteString(out, "\n"); err != nil {
				return err
			}
		}
		if len(reports) > 1 {
			if _, err := io.WriteString(out, "HPA "+report.Analysis.Namespace+"/"+report.Analysis.Name+"\n"); err != nil {
				return err
			}
		}
		if err := hpaanalysis.WriteDecisionTraceText(out, report.Analysis.DecisionTrace); err != nil {
			return err
		}
	}
	return nil
}
