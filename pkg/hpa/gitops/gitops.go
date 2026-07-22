// Package gitops detects conflicts and reviews risky changes between
// GitOps-managed manifests and live HPA state. It is a self-contained leaf
// domain depending only on standard library and Kubernetes API types. The
// cmd/ layer calls it directly (gitops.AnalyzeConflict, gitops.AnalyzeReview,
// etc.).
package gitops

import "fmt"

// Conflict holds the result of GitOps manifest conflict analysis.
type Conflict struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Conflicts lists all detected conflicts between manifests and HPA state.
	Conflicts []ConflictEntry `json:"conflicts,omitempty" yaml:"conflicts,omitempty"`
	// Warnings lists advisory findings that don't block GitOps sync.
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	// Patches lists suggested manifest patches to resolve conflicts.
	Patches []string `json:"patches,omitempty" yaml:"patches,omitempty"`
	// Summary is a one-line summary of the GitOps conflict state.
	Summary string `json:"summary" yaml:"summary"`
}

// ConflictEntry represents a single conflict or finding.
type ConflictEntry struct {
	// Kind is the resource kind (Deployment, StatefulSet).
	Kind string `json:"kind" yaml:"kind"`
	// Name is the resource name.
	Name string `json:"name" yaml:"name"`
	// Field is the conflicting field path.
	Field string `json:"field" yaml:"field"`
	// ManifestValue is the value set in the Git manifest.
	ManifestValue string `json:"manifestValue,omitempty" yaml:"manifestValue,omitempty"`
	// LiveValue is the current value in the cluster.
	LiveValue string `json:"liveValue,omitempty" yaml:"liveValue,omitempty"`
	// HPADesired is the replica count the HPA wants.
	HPADesired string `json:"hpaDesired,omitempty" yaml:"hpaDesired,omitempty"`
	// Severity is the finding severity: "conflict", "warning", "info".
	Severity string `json:"severity" yaml:"severity"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	// Remediation suggests a fix for the conflict.
	Remediation string `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

// Input aggregates signals for GitOps conflict detection. The cmd layer
// assembles this from manifest files and live K8s state, keeping pkg/hpa free
// of Kubernetes client dependencies.
type Input struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// TargetKind is the scale target kind (Deployment, StatefulSet).
	TargetKind string
	// TargetName is the scale target name.
	TargetName string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// ManifestReplicas is the spec.replicas from the manifest file (nil if not set).
	ManifestReplicas *int32
	// LiveReplicas is the current replica count from the live scale target.
	LiveReplicas int32
	// ArgoCDAnnotations are annotations indicating Argo CD management.
	ArgoCDAnnotations map[string]string
	// FluxAnnotations are annotations indicating Flux management.
	FluxAnnotations map[string]string
	// KEDAManaged indicates the workload is managed by KEDA.
	KEDAManaged bool
}

// AnalyzeConflict detects conflicts between GitOps manifests and HPA
// scaling decisions. It examines manifest replicas, GitOps tool annotations,
// and generates actionable remediation steps.
func AnalyzeConflict(input Input) *Conflict {
	result := &Conflict{
		Namespace: input.Namespace,
		Name:      input.HPAName,
		Target:    fmt.Sprintf("%s/%s", input.TargetKind, input.TargetName),
		Conflicts: make([]ConflictEntry, 0),
		Warnings:  make([]string, 0),
		Patches:   make([]string, 0),
	}

	// Check for manifest replicas conflict
	if input.ManifestReplicas != nil && input.DesiredReplicas != *input.ManifestReplicas {
		result.Conflicts = append(result.Conflicts, ConflictEntry{
			Kind:          input.TargetKind,
			Name:          input.TargetName,
			Field:         "spec.replicas",
			ManifestValue: fmt.Sprintf("%d", *input.ManifestReplicas),
			LiveValue:     fmt.Sprintf("%d", input.LiveReplicas),
			HPADesired:    fmt.Sprintf("%d", input.DesiredReplicas),
			Severity:      "conflict",
			Detail: fmt.Sprintf("Next GitOps sync may reset replicas from %d to %d",
				input.DesiredReplicas, *input.ManifestReplicas),
			Remediation: fmt.Sprintf("Remove spec.replicas from %s manifest, or set minReplicas=%d on the HPA",
				input.TargetKind, *input.ManifestReplicas),
		})
		result.Patches = append(result.Patches,
			fmt.Sprintf("spec.replicas: null # remove from %s/%s to allow HPA control", input.TargetKind, input.TargetName))
	}

	// Check for Argo CD annotations
	if len(input.ArgoCDAnnotations) > 0 {
		keys := annotationKeys(input.ArgoCDAnnotations)
		result.Conflicts = append(result.Conflicts, ConflictEntry{
			Kind:        input.TargetKind,
			Name:        input.TargetName,
			Severity:    "info",
			Detail:      fmt.Sprintf("Argo CD managed (annotations: %s)", keys),
			Remediation: "Changes should be applied via Git, not kubectl patch",
		})
		result.Warnings = append(result.Warnings,
			"Argo CD sync may override manual changes; commit HPA adjustments to Git")
	}

	// Check for Flux annotations
	if len(input.FluxAnnotations) > 0 {
		keys := annotationKeys(input.FluxAnnotations)
		result.Conflicts = append(result.Conflicts, ConflictEntry{
			Kind:        input.TargetKind,
			Name:        input.TargetName,
			Severity:    "info",
			Detail:      fmt.Sprintf("Flux managed (annotations: %s)", keys),
			Remediation: "Changes should be applied via Git, not kubectl patch",
		})
		result.Warnings = append(result.Warnings,
			"Flux Kustomize/Helm sync may override manual changes; commit HPA adjustments to Git")
	}

	// Check for KEDA management
	if input.KEDAManaged {
		result.Conflicts = append(result.Conflicts, ConflictEntry{
			Kind:        input.TargetKind,
			Name:        input.TargetName,
			Severity:    "info",
			Detail:      "KEDA managed workload",
			Remediation: "Direct spec.replicas patches will be overwritten; modify ScaledObject instead",
		})
		result.Warnings = append(result.Warnings,
			"KEDA controls replica count based on external triggers; HPA acts as fallback")
	}

	// Build summary
	conflictCount := len(result.Conflicts)
	warningCount := len(result.Warnings)
	if conflictCount == 0 && warningCount == 0 {
		result.Summary = "No GitOps conflicts detected"
	} else {
		result.Summary = fmt.Sprintf("Found %d conflict(s), %d warning(s)", conflictCount, warningCount)
	}

	return result
}

// annotationKeys returns a comma-separated list of annotation keys.
func annotationKeys(annotations map[string]string) string {
	keys := make([]string, 0, len(annotations))
	for k := range annotations {
		keys = append(keys, k)
	}
	if len(keys) == 1 {
		return keys[0]
	}
	// Return first few keys if many
	if len(keys) > 3 {
		return fmt.Sprintf("%s, ...", keys[0])
	}
	return fmt.Sprintf("%s, %s", keys[0], keys[1])
}
