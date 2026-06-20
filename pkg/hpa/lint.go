package hpa

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/lint"
)

// This file is a thin re-export facade for the lint domain, which now lives
// in pkg/hpa/lint. The types and functions below preserve the existing
// hpaanalysis.* API surface. The canonical implementations are in
// pkg/hpa/lint/lint.go. The lint_text.go renderer stays in pkg/hpa because it
// shares the labels machinery.

// Lint domain type aliases.
type (
	// LintSeverity aliases lint.Severity.
	LintSeverity = lint.Severity
	// LintFinding aliases lint.Finding.
	LintFinding = lint.Finding
	// LintResult aliases lint.Result.
	LintResult = lint.Result
	// LintAutoFix aliases lint.AutoFix.
	LintAutoFix = lint.AutoFix
)

// Lint severity constants.
const (
	LintErrorSeverity   = lint.Error
	LintWarningSeverity = lint.Warning
	LintInfoSeverity    = lint.Info
	LintError           = lint.Error
	LintWarning         = lint.Warning
	LintInfo            = lint.Info
)

// LintHPA runs all lint rules against the HPA. Delegates to lint.Run.
func LintHPA(hpa *autoscalingv2.HorizontalPodAutoscaler) *LintResult {
	return lint.Run(hpa)
}

// FormatLintSARIF formats lint results as SARIF. Delegates to lint.FormatLintSARIF.
func FormatLintSARIF(result *LintResult, filePath string) string {
	return lint.FormatLintSARIF(result, filePath)
}
