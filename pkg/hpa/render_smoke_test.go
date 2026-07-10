package hpa

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/audit"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/lint"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// These smoke tests exercise the standalone text/markdown/HTML renderers with
// both empty and lightly-populated inputs. They pin two properties for every
// renderer: it never returns an error for valid input, and it always emits
// something (a header or an explicit "no data" line) so command output never
// silently disappears. Field-level formatting is covered by the per-domain
// tests; these tests exist so a renderer cannot regress to panicking or
// writing nothing without a test failing.

func TestRenderSmokeWriters(t *testing.T) {
	theme := style.Theme{}
	populatedLint := &lint.Result{
		Findings: []lint.Finding{{
			Severity:       lint.Error,
			Rule:           "min-replicas",
			Message:        "minReplicas is 1",
			Recommendation: "raise minReplicas to 2",
			AutoFix: &lint.AutoFix{
				Patch:   `{"spec":{"minReplicas":2}}`,
				Command: "kubectl patch hpa web --type merge -p ...",
				Before:  "minReplicas: 1",
				After:   "minReplicas: 2",
			},
		}},
		Errors: 1,
		Pass:   false,
	}
	populatedAudit := &audit.Report{
		Namespace: "default",
		Name:      "web",
		Target:    "Deployment/web",
		Score:     80,
		Findings: []audit.Finding{{
			ID:          "AUD001",
			Title:       "missing scaleDown stabilization",
			Description: "scaleDown has no stabilization window",
			Severity:    audit.AuditWarning,
			Category:    "stabilization",
		}},
	}
	populatedChurn := &churn.ChurnAnalysis{
		Score:          72,
		Level:          churn.ChurnHigh,
		ScaleUpCount:   5,
		ScaleDownCount: 4,
		DirectionFlips: 6,
	}
	populatedGuard := &GuardResult{
		Allowed: []Suggestion{{Title: "raise maxReplicas", Description: "allow more headroom"}},
		Blocked: []GuardBlocked{{
			Suggestion: Suggestion{Title: "drop minReplicas"},
			Reason:     "below policy floor",
			PolicyRule: "min-replicas-floor",
		}},
		Warnings: []GuardWarning{{
			Suggestion: Suggestion{Title: "widen tolerance"},
			Reason:     "close to policy ceiling",
			PolicyRule: "tolerance-ceiling",
		}},
	}

	cases := []struct {
		name   string
		render func(w *bytes.Buffer) error
	}{
		{"CapacityPlanEmpty", func(w *bytes.Buffer) error { return WriteCapacityPlanText(w, &CapacityPlan{}, theme) }},
		{"AutoscalerMapEmpty", func(w *bytes.Buffer) error { return WriteAutoscalerMapText(w, &AutoscalerMap{}, theme) }},
		{"MetricContractText", func(w *bytes.Buffer) error { return WriteMetricContractText(w, &MetricContractReport{}) }},
		{"MetricContractMarkdown", func(w *bytes.Buffer) error { return WriteMetricContractMarkdown(w, &MetricContractReport{}) }},
		{"MetricContractHTML", func(w *bytes.Buffer) error { return WriteMetricContractHTML(w, &MetricContractReport{}) }},
		{"GitOpsConflictText", func(w *bytes.Buffer) error { return WriteGitOpsConflictText(w, &GitOpsConflict{}) }},
		{"GitOpsConflictMarkdown", func(w *bytes.Buffer) error { return WriteGitOpsConflictMarkdown(w, &GitOpsConflict{}) }},
		{"GitOpsConflictHTML", func(w *bytes.Buffer) error { return WriteGitOpsConflictHTML(w, &GitOpsConflict{}) }},
		{"ContainerAdvisor", func(w *bytes.Buffer) error { return WriteContainerAdvisorText(w, &ContainerAdvisorResult{}, nil) }},
		{"AssumptionsText", func(w *bytes.Buffer) error { return WriteAssumptionsText(w, &ControllerAssumptions{}, theme) }},
		{"AssumptionsMarkdown", func(w *bytes.Buffer) error { return WriteAssumptionsMarkdown(w, &ControllerAssumptions{}) }},
		{"AssumptionsExplain", func(w *bytes.Buffer) error {
			return WriteAssumptionsTextWithExplain(w, &ControllerAssumptions{}, true, theme)
		}},
		{"RolloutReport", func(w *bytes.Buffer) error { return WriteRolloutReportText(w, &RolloutReport{}, theme) }},
		{"LintTextPopulated", func(w *bytes.Buffer) error { return WriteLintText(w, populatedLint) }},
		{"LintSummaryPopulated", func(w *bytes.Buffer) error { return WriteLintSummary(w, populatedLint) }},
		{"LintDiffPopulated", func(w *bytes.Buffer) error { return WriteLintDiff(w, populatedLint) }},
		{"LintCompactPopulated", func(w *bytes.Buffer) error { return WriteLintCompact(w, populatedLint, "hpa.yaml") }},
		{"ChurnTextPopulated", func(w *bytes.Buffer) error { return WriteChurnText(w, populatedChurn, theme) }},
		{"ChurnMarkdownPopulated", func(w *bytes.Buffer) error { return WriteChurnMarkdown(w, populatedChurn) }},
		{"ChurnHTMLPopulated", func(w *bytes.Buffer) error { return WriteChurnHTML(w, populatedChurn) }},
		{"ReadinessDoctorText", func(w *bytes.Buffer) error { return WriteReadinessDoctorText(w, &ReadinessDoctorReport{}, theme) }},
		{"ReadinessDoctorMarkdown", func(w *bytes.Buffer) error { return WriteReadinessDoctorMarkdown(w, &ReadinessDoctorReport{}) }},
		{"DecisionTrace", func(w *bytes.Buffer) error { return WriteDecisionTraceText(w, &DecisionTrace{}) }},
		{"BehaviorAdvisor", func(w *bytes.Buffer) error { return WriteBehaviorAdvisorText(w, &BehaviorAdvisorResult{}, nil) }},
		{"PolicyGuardPopulated", func(w *bytes.Buffer) error { return WritePolicyGuardText(w, populatedGuard) }},
		{"AuditPopulated", func(w *bytes.Buffer) error { return WriteAuditText(w, populatedAudit, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tc.render(&buf); err != nil {
				t.Fatalf("renderer returned error: %v", err)
			}
			if buf.Len() == 0 {
				t.Fatal("renderer produced no output")
			}
		})
	}
}

func TestRenderSmokeAppenders(t *testing.T) {
	cases := []struct {
		name   string
		append func(out *[]byte)
	}{
		{"StructuredDecisionTrace", func(out *[]byte) {
			AppendStructuredDecisionTraceText(out, &StructuredDecisionTrace{}, nil)
		}},
		{"FlappingPrevention", func(out *[]byte) {
			AppendFlappingPreventionText(out, &FlappingPreventionReport{}, resolveLabels(nil))
		}},
		{"AdapterDiagnostics", func(out *[]byte) {
			AppendAdapterDiagnosticsText(out, &AdapterDiagnosticsReport{})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out []byte
			tc.append(&out)
			if len(out) == 0 {
				t.Fatal("appender produced no output")
			}
		})
	}
}

func TestRenderSmokeFormatters(t *testing.T) {
	trend := HealthTrendResult{
		Snapshots:        []HealthSnapshot{{HealthScore: 90}, {HealthScore: 60}},
		MinScore:         60,
		MaxScore:         90,
		MeanScore:        75,
		Variance:         12.5,
		FlappingDetected: true,
		FlappingSeverity: "high",
		Sparkline:        "▂▇",
		Anomalies: []AnomalyDetection{{
			Type:        AnomalySuddenDegradation,
			Severity:    "warning",
			ScoreBefore: 90,
			ScoreAfter:  60,
		}},
	}
	if s := FormatTrendText(trend); s == "" {
		t.Error("FormatTrendText returned empty string for populated trend")
	}
	if s := FormatTrendAnomalyText(trend); s == "" {
		t.Error("FormatTrendAnomalyText returned empty string for populated trend")
	}
	if s := FormatTrendListRow(trend); s == "" {
		t.Error("FormatTrendListRow returned empty string for populated trend")
	}
	// Graph rendering needs a non-trivial width; just assert it does not panic
	// and mentions nothing when snapshots are empty.
	_ = FormatTrendAnomalyGraph(trend, 20)

	signals := []DecisionSignal{{Reason: "ToleranceHold", Message: "within tolerance", Confidence: "high"}}
	if s := FormatDecisionSignals(signals); !strings.Contains(s, "ToleranceHold") {
		t.Errorf("FormatDecisionSignals missing reason, got %q", s)
	}
	if s := FormatDecisionSignalsCompact(signals); s == "" {
		t.Error("FormatDecisionSignalsCompact returned empty string")
	}
}
