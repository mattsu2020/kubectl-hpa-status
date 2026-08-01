// Package compat builds the Kubernetes/HPA feature-compatibility report shown
// by the `compat` command. It depends only on a discovery client, so the
// report model and its rules are testable without cobra, without the cmd
// option struct, and without a full Kubernetes client.
//
// Lifted from cmd/compat.go as part of the cmd/ sub-package split; cmd/ keeps
// the cobra wiring and the output-format routing.
package compat

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"k8s.io/client-go/discovery"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
)

// Report is the compat command's output document.
type Report struct {
	ClusterVersion string        `json:"clusterVersion" yaml:"clusterVersion"`
	HPAAPI         string        `json:"hpaApi" yaml:"hpaApi"`
	Checks         []CheckResult `json:"checks" yaml:"checks"`
}

// CheckResult is a single compatibility finding. Status is one of OK, WARN, or
// ERROR.
type CheckResult struct {
	Status  string `json:"status" yaml:"status"`
	Feature string `json:"feature" yaml:"feature"`
	Message string `json:"message" yaml:"message"`
}

// Check builds a CheckResult. Exported so tests can construct the expected
// findings from the same helper the rules use.
func Check(status, feature, message string) CheckResult {
	return CheckResult{Status: status, Feature: feature, Message: message}
}

// BuildReport queries the discovery API and evaluates the compatibility rules.
// Discovery failures become WARN findings rather than errors: a partially
// answerable report is more useful than none, and an RBAC denial must not be
// silently reported as an old cluster.
func BuildReport(_ context.Context, disco discovery.DiscoveryInterface) Report {
	report := Report{HPAAPI: "unknown"}
	if version, err := disco.ServerVersion(); err == nil {
		report.ClusterVersion = version.GitVersion
	} else {
		// Surface the discovery failure rather than silently reporting "unknown",
		// so an RBAC denial or unreachable API is not mistaken for an old cluster.
		report.Checks = append(report.Checks, Check("WARN", "cluster version discovery", fmt.Sprintf("server version query failed: %v", err)))
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
		report.Checks = append(report.Checks, Check("WARN", "HPA API discovery", fmt.Sprintf("autoscaling/v2 lookup failed: %v", err)))
	}
	minor := ParseKubeMinor(report.ClusterVersion)
	vers := kube.KubernetesVersions()
	report.Checks = append(report.Checks,
		Check("OK", "multiple metrics", "supported by autoscaling/v2"),
		Check("OK", "containerResource metrics", "stable in Kubernetes v"+vers.ContainerResourceVer+"+"),
	)
	switch {
	case minor >= vers.ToleranceFeatureMinor:
		report.Checks = append(report.Checks, Check("OK", "behavior scaleUp/scaleDown tolerance", "available as Kubernetes v"+vers.ToleranceFeatureVer+"+ beta field when feature gate is enabled"))
	case minor > 0:
		report.Checks = append(report.Checks, Check("WARN", "behavior scaleUp/scaleDown tolerance", "requires Kubernetes v"+vers.ToleranceFeatureVer+"+ and HPAConfigurableTolerance"))
	default:
		report.Checks = append(report.Checks, Check("WARN", "behavior scaleUp/scaleDown tolerance", "cluster version unknown; requires Kubernetes v"+vers.ToleranceFeatureVer+"+"))
	}
	if report.HPAAPI != "autoscaling/v2" {
		report.Checks = append(report.Checks, Check("ERROR", "HPA API", "autoscaling/v2 was not discovered"))
	}
	return report
}

// WriteText renders the human-readable form of the report.
func WriteText(out io.Writer, report Report) error {
	if _, err := fmt.Fprintf(out, "Cluster: %s\nHPA API: %s\n\nCompatibility:\n", report.ClusterVersion, report.HPAAPI); err != nil {
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
}

// ParseKubeMinor returns the Kubernetes minor version parsed from a GitVersion
// string (e.g. "v1.35.1" -> 35). Returns 0 when the value cannot be parsed,
// which callers treat as "unknown".
func ParseKubeMinor(version string) int {
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
