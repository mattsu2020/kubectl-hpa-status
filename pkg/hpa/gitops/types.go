package gitops

import autoscalingv2 "k8s.io/api/autoscaling/v2"

// ---------------------------------------------------------------------------
// GitOps Review types (gitops review command)
// ---------------------------------------------------------------------------

// Review holds the result of reviewing HPA manifest changes for
// risky modifications in a PR or GitOps diff.
type Review struct {
	// Files lists the files that were reviewed.
	Files []ReviewFile `json:"files,omitempty" yaml:"files,omitempty"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// RiskLevel is the overall risk level: "high", "medium", "low", "none".
	RiskLevel string `json:"riskLevel" yaml:"riskLevel"`
	// Findings lists all detected risky changes.
	Findings []ReviewFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
}

// ReviewFile holds review results for a single file.
type ReviewFile struct {
	// Path is the file path.
	Path string `json:"path" yaml:"path"`
	// HPAName is the HPA name from the manifest.
	HPAName string `json:"hpaName,omitempty" yaml:"hpaName,omitempty"`
	// Findings lists findings for this file.
	Findings []ReviewFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
}

// ReviewFinding represents a single risky HPA manifest change.
type ReviewFinding struct {
	// Severity is the finding severity: "high", "medium", "low".
	Severity string `json:"severity" yaml:"severity"`
	// Category is the finding category (e.g. "maxReplicas", "stabilization",
	// "target", "behavior", "metric").
	Category string `json:"category" yaml:"category"`
	// Message describes the finding.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// ReviewInput holds the before and after HPA manifests for diff review.
type ReviewInput struct {
	// Before is the base (old) HPA manifest.
	Before *autoscalingv2.HorizontalPodAutoscaler
	// After is the proposed (new) HPA manifest.
	After *autoscalingv2.HorizontalPodAutoscaler
	// FilePath is the path to the manifest file.
	FilePath string
}
