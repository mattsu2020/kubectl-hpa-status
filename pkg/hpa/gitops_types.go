package hpa

import autoscalingv2 "k8s.io/api/autoscaling/v2"

// ---------------------------------------------------------------------------
// GitOps Review types (gitops review command)
// ---------------------------------------------------------------------------

// GitOpsReview holds the result of reviewing HPA manifest changes for
// risky modifications in a PR or GitOps diff.
type GitOpsReview struct {
	// Files lists the files that were reviewed.
	Files []GitOpsReviewFile `json:"files,omitempty" yaml:"files,omitempty"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// RiskLevel is the overall risk level: "high", "medium", "low", "none".
	RiskLevel string `json:"riskLevel" yaml:"riskLevel"`
	// Findings lists all detected risky changes.
	Findings []GitOpsReviewFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
}

// GitOpsReviewFile holds review results for a single file.
type GitOpsReviewFile struct {
	// Path is the file path.
	Path string `json:"path" yaml:"path"`
	// HPAName is the HPA name from the manifest.
	HPAName string `json:"hpaName,omitempty" yaml:"hpaName,omitempty"`
	// Findings lists findings for this file.
	Findings []GitOpsReviewFinding `json:"findings,omitempty" yaml:"findings,omitempty"`
}

// GitOpsReviewFinding represents a single risky HPA manifest change.
type GitOpsReviewFinding struct {
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

// GitOpsReviewInput holds the before and after HPA manifests for diff review.
type GitOpsReviewInput struct {
	// Before is the base (old) HPA manifest.
	Before *autoscalingv2.HorizontalPodAutoscaler
	// After is the proposed (new) HPA manifest.
	After *autoscalingv2.HorizontalPodAutoscaler
	// FilePath is the path to the manifest file.
	FilePath string
}
