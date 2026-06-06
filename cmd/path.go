package cmd

import (
	"context"
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type scalePathReport struct {
	Namespace string                 `json:"namespace" yaml:"namespace"`
	Name      string                 `json:"name" yaml:"name"`
	Target    string                 `json:"target" yaml:"target"`
	ScalePath *hpaanalysis.ScalePath `json:"scalePath" yaml:"scalePath"`
}

func newPathCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "path NAME [NAME...]",
		Short:             "Explain the HPA scale path from desired replicas to pods and scheduler capacity",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPath(cmd.Context(), cmd.OutOrStdout(), opts, args)
		},
	}
}

func runPath(ctx context.Context, out io.Writer, opts *options, names []string) error {
	opts.scalePath = true
	reports := make([]scalePathReport, 0, len(names))
	for _, name := range names {
		report, err := buildStatusReportWithClient(ctx, opts, name, false, nil)
		if err != nil {
			if opts.output == "json" || opts.output == "yaml" {
				writeError(out, opts.output, err)
			}
			return err
		}
		reports = append(reports, scalePathReport{
			Namespace: report.Analysis.Namespace,
			Name:      report.Analysis.Name,
			Target:    report.Analysis.Target,
			ScalePath: report.Analysis.ScalePath,
		})
	}

	value := any(reports)
	if len(reports) == 1 {
		value = reports[0]
	}
	format, templateStr := outputSelection(outputConfig{
		output: opts.output, template: opts.template, outputTemplates: opts.outputTemplates,
	})
	return writeOutput(out, format, templateStr, value, func() error {
		for i, report := range reports {
			if i > 0 {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if len(reports) > 1 {
				if _, err := fmt.Fprintf(out, "HPA %s/%s\n", report.Namespace, report.Name); err != nil {
					return err
				}
			}
			if err := hpaanalysis.WriteScalePathText(out, report.ScalePath); err != nil {
				return err
			}
		}
		return nil
	})
}
