package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/cmd/internal/compat"
	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/render"
)

// The compat report model, its rules, and its text renderer live in
// cmd/internal/compat. This file keeps only the cobra wiring,
// discovery-client construction, and output-format routing.
func newCompatCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "compat",
		Short: "Check Kubernetes/HPA feature compatibility",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCompat(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
}

func runCompat(ctx context.Context, out io.Writer, opts *options) error {
	disco, err := kube.NewDiscoveryClient(opts.KubeOptions())
	if err != nil {
		return err
	}
	report := compat.BuildReport(ctx, disco)
	return render.Format(out, opts.Output, opts.Template, report, func(out io.Writer) error {
		return compat.WriteText(out, report)
	})
}
