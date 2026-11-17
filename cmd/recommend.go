package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
)

func newRecommendCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "recommend NAME [NAME...]",
		Short:             "Audit HPA configuration against best practices",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, profileErr := flagString(cmd, "profile")
			if profileErr != nil {
				return profileErr
			}
			policyFile, policyErr := flagString(cmd, "policy")
			if policyErr != nil {
				return policyErr
			}
			if policyFile != "" {
				return runPolicyMany(cmd.Context(), cmd.OutOrStdout(), opts, &policyCommandOptions{file: policyFile}, args...)
			}
			return runRecommend(cmd.Context(), cmd.OutOrStdout(), opts, args, audit.Profile(profile))
		},
	}
	cmd.Flags().String("profile", "", "workload profile for threshold adjustments: latency, cost, batch, keda, critical")
	cmd.Flags().String("policy", "", "policy YAML file for organization-specific HPA rules")
	return cmd
}

func runRecommend(ctx context.Context, out io.Writer, opts *options, args []string, profile audit.Profile) error {
	client, err := newClientOrDefault(opts)
	if err != nil {
		return err
	}

	reports, err := collectPerHPA(ctx, opts, args, func(ctx context.Context, hpaName string) (*audit.Report, error) {
		hpa, err := kube.GetHPAFromClient(ctx, client, hpaName)
		if err != nil {
			return nil, wrapHPALookupError(client.Namespace, hpaName, err)
		}

		minReplicas := hpaanalysis.DefaultMinReplicas
		if hpa.Spec.MinReplicas != nil {
			minReplicas = *hpa.Spec.MinReplicas
		}

		if profile != "" {
			return audit.RunWithProfile(hpa, minReplicas, profile), nil
		}
		return audit.Run(hpa, minReplicas), nil
	})
	if err != nil {
		return err
	}

	return renderPerHPA(out, opts, reports, func(w io.Writer, report *audit.Report) error {
		return audit.WriteText(w, report, labelProviderForLang(opts.Lang, opts.Output))
	})
}
