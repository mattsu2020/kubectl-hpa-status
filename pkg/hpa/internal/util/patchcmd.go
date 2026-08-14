// Package util holds small, dependency-free helpers shared across the pkg/hpa
// analysis domains.
package util

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DryRunMode controls whether the generated kubectl patch command runs against
// the server, the client, or neither (a live apply).
type DryRunMode int

const (
	// DryRunServer appends --dry-run=server (validates against the live
	// apiserver without persisting). This is the safest default for copy-paste
	// suggestions and matches the historical behavior of KubectlPatchCommand.
	DryRunServer DryRunMode = iota
	// DryRunClient appends --dry-run=client (local validation only).
	DryRunClient
	// DryRunNone omits the --dry-run flag so the patch is a live apply.
	DryRunNone
)

// KubectlPatchCommand formats a kubectl patch command string for an HPA with
// the given JSON merge patch, including --dry-run=server. It is the canonical
// builder for suggestion/audit output; prefer it over hand-rolled
// fmt.Sprintf("kubectl patch hpa ...") so the flag spelling stays consistent.
func KubectlPatchCommand(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string) string {
	return KubectlPatchCommandWithDryRun(hpa, patch, DryRunServer)
}

// KubectlPatchCommandWithDryRun is like KubectlPatchCommand but lets the caller
// pick the dry-run mode. Use DryRunServer for safe copy-paste suggestions,
// DryRunClient for fully-local validation, and DryRunNone when the surrounding
// context already gates the apply (e.g. behind an explicit --apply flag).
func KubectlPatchCommandWithDryRun(hpa *autoscalingv2.HorizontalPodAutoscaler, patch string, mode DryRunMode) string {
	command := fmt.Sprintf("kubectl patch hpa %s -n %s --type=merge -p '%s'", hpa.Name, hpa.Namespace, patch)
	switch mode {
	case DryRunServer:
		command += " --dry-run=server"
	case DryRunClient:
		command += " --dry-run=client"
	case DryRunNone:
		// no flag
	}
	return command
}
