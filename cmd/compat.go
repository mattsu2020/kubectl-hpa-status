package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/discovery"
)

type compatReport struct {
	ClusterVersion string              `json:"clusterVersion" yaml:"clusterVersion"`
	HPAAPI         string              `json:"hpaApi" yaml:"hpaApi"`
	Checks         []compatCheckResult `json:"checks" yaml:"checks"`
}

type compatCheckResult struct {
	Status  string `json:"status" yaml:"status"`
	Feature string `json:"feature" yaml:"feature"`
	Message string `json:"message" yaml:"message"`
}

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
	disco, err := kube.NewDiscoveryClient(kube.Options{
		Namespace:  opts.Namespace,
		Context:    opts.ContextName,
		Kubeconfig: opts.Kubeconfig,
		Cluster:    opts.Cluster,
		QPS:        opts.QPS,
		Burst:      opts.Burst,
	})
	if err != nil {
		return err
	}
	report := buildCompatReport(ctx, disco)
	return writeOutput(out, opts.Output, opts.Template, report, func() error {
		_, err := fmt.Fprintf(out, "Cluster: %s\nHPA API: %s\n\nCompatibility:\n", report.ClusterVersion, report.HPAAPI)
		if err != nil {
			return err
		}
		for _, check := range report.Checks {
			if _, err := fmt.Fprintf(out, "  %s: %s", check.Status, check.Feature); err != nil {
				return err
			}
			if check.Message != "" {
				if _, err := fmt.Fprintf(out, " - %s", check.Message); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildCompatReport(_ context.Context, disco discovery.DiscoveryInterface) compatReport {
	report := compatReport{HPAAPI: "unknown"}
	if version, err := disco.ServerVersion(); err == nil {
		report.ClusterVersion = version.GitVersion
	} else {
		// Surface the discovery failure rather than silently reporting "unknown",
		// so an RBAC denial or unreachable API is not mistaken for an old cluster.
		report.Checks = append(report.Checks, compatCheck("WARN", "cluster version discovery", fmt.Sprintf("server version query failed: %v", err)))
	}
	if report.ClusterVersion == "" {
		report.ClusterVersion = "unknown"
	}
	if resources, err := disco.ServerResourcesForGroupVersion("autoscaling/v2"); err == nil {
		for _, r := range resources.APIResources {
			if r.Kind == "HorizontalPodAutoscaler" {
				report.HPAAPI = "autoscaling/v2"
				break
			}
		}
	} else {
		// Distinguish "API genuinely absent" (handled by the ERROR check below)
		// from "the discovery call itself failed". Without this, an RBAC denial
		// looks identical to a cluster that lacks autoscaling/v2.
		report.Checks = append(report.Checks, compatCheck("WARN", "HPA API discovery", fmt.Sprintf("autoscaling/v2 lookup failed: %v", err)))
	}
	minor := parseKubeMinor(report.ClusterVersion)
	vers := kube.KubernetesVersions()
	report.Checks = append(report.Checks,
		compatCheck("OK", "multiple metrics", "supported by autoscaling/v2"),
		compatCheck("OK", "containerResource metrics", "stable in Kubernetes v"+vers.ContainerResourceVer+"+"),
	)
	switch {
	case minor >= vers.ToleranceFeatureMinor:
		report.Checks = append(report.Checks, compatCheck("OK", "behavior scaleUp/scaleDown tolerance", "available as Kubernetes v"+vers.ToleranceFeatureVer+"+ beta field when feature gate is enabled"))
	case minor > 0:
		report.Checks = append(report.Checks, compatCheck("WARN", "behavior scaleUp/scaleDown tolerance", "requires Kubernetes v"+vers.ToleranceFeatureVer+"+ and HPAConfigurableTolerance"))
	default:
		report.Checks = append(report.Checks, compatCheck("WARN", "behavior scaleUp/scaleDown tolerance", "cluster version unknown; requires Kubernetes v"+vers.ToleranceFeatureVer+"+"))
	}
	if report.HPAAPI != "autoscaling/v2" {
		report.Checks = append(report.Checks, compatCheck("ERROR", "HPA API", "autoscaling/v2 was not discovered"))
	}
	return report
}

func compatCheck(status, feature, message string) compatCheckResult {
	return compatCheckResult{Status: status, Feature: feature, Message: message}
}

// parseKubeMinor returns the Kubernetes minor version parsed from a GitVersion
// string (e.g. "v1.35.1" -> 35). Returns 0 when the value cannot be parsed,
// which the caller treats as "unknown".
func parseKubeMinor(version string) int {
	version = strings.TrimPrefix(version, "v")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0
	}
	minorStr := strings.TrimRightFunc(parts[1], func(r rune) bool {
		return r < '0' || r > '9'
	})
	minor, _ := strconv.Atoi(minorStr)
	return minor
}

// markFlagDeprecated annotates a flag so that its --help text and the generated
// docs make the deprecation visible. pflag has no first-class Deprecated field,
// so the convention here is to prefix the usage string with "[deprecated] " so
// the rename is visible to users running --help. The runtime stderr notice is
// emitted by internal/cmdoptions.warnDeprecatedOnce when the alias is actually
// used. Removal of the flagged aliases is scheduled for v2.0; see ROADMAP.md.
func markFlagDeprecated(flags *pflag.FlagSet, name, reason string) {
	if f := flags.Lookup(name); f != nil {
		f.Usage = fmt.Sprintf("[deprecated] %s", reason)
		f.Hidden = false // keep visible in --help so users learn about the rename
	}
}
