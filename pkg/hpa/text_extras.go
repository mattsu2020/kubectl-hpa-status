package hpa

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// This file holds the smaller section renderers that were previously inlined
// in WriteStatusTextWithOptions (text.go). Extracting them lets the
// orchestrator stay a flat list of section calls without a gocyclo exemption.

func appendHealthTrendSection(out *[]byte, a *Analysis) {
	if a.HealthTrend == nil || len(a.HealthTrend.Snapshots) == 0 {
		return
	}
	*out = append(*out, '\n')
	trendText := FormatTrendText(*a.HealthTrend)
	*out = fmt.Appendf(*out, "%s\n", trendText)
}

func appendControllerProfileSection(out *[]byte, a *Analysis) {
	if a.ControllerProfile == nil {
		return
	}
	*out = append(*out, '\n')
	appendControllerProfileText(out, a.ControllerProfile)
}

func appendActionsSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Actions) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Actions)
	for _, action := range a.Actions {
		*out = fmt.Appendf(*out, "  - %s\n", theme.ActionLine(action))
	}
}

func appendInterpretationSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Interpretation) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Interpretation)
	for _, line := range a.Interpretation {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(line))
	}
}

func appendDebugSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Debug) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Debug)
	for _, line := range a.Debug {
		*out = fmt.Appendf(*out, "  - %s\n", theme.Dim.Render(line))
	}
}

func appendDecisionSignalsSection(out *[]byte, a *Analysis) {
	if len(a.DecisionSignals) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s\n", FormatDecisionSignals(a.DecisionSignals))
}

func appendDecisionTraceSection(out *[]byte, a *Analysis) {
	if a.DecisionTrace == nil {
		return
	}
	*out = append(*out, '\n')
	AppendDecisionTraceText(out, a.DecisionTrace)
}

func appendStructuredDecisionTraceSection(out *[]byte, a *Analysis, opts StatusTextOptions) {
	if a.StructuredDecisionTrace == nil {
		return
	}
	*out = append(*out, '\n')
	AppendStructuredDecisionTraceText(out, a.StructuredDecisionTrace, opts.Labels)
}

func appendAdapterDiagnosticsSection(out *[]byte, a *Analysis) {
	if a.AdapterDiagnostics == nil {
		return
	}
	*out = append(*out, '\n')
	AppendAdapterDiagnosticsText(out, a.AdapterDiagnostics)
}

func appendCapacityHeadroomSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.CapacityHeadroom == nil {
		return
	}
	*out = append(*out, '\n')
	appendCapacityHeadroomText(out, a.CapacityHeadroom, theme)
}

func appendReadinessImpactSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.ReadinessImpact == nil {
		return
	}
	*out = append(*out, '\n')
	appendReadinessImpactText(out, a.ReadinessImpact, theme)
}

func appendScalePathSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.ScalePath == nil {
		return
	}
	*out = append(*out, '\n')
	appendScalePathText(out, a.ScalePath, theme)
}

func appendRolloutDiagnosisSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.RolloutDiagnosis == nil {
		return
	}
	*out = append(*out, '\n')
	appendRolloutDiagnosisText(out, a.RolloutDiagnosis, theme)
}

func appendBlockerReportSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if a.BlockerReport == nil {
		return
	}
	AppendBlockerText(out, a.BlockerReport, theme, labels)
	appendScaleoutBlockersText(out, a.BlockerReport, theme)
}

func appendCapacityPlanSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if a.CapacityPlan == nil {
		return
	}
	AppendCapacityPlanText(out, a.CapacityPlan, theme, labels)
}

func appendMetricContractSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.MetricContract == nil {
		return
	}
	*out = append(*out, '\n')
	appendMetricContractText(out, a.MetricContract, theme)
}

func appendContainerAdvisorSection(out *[]byte, a *Analysis, labels labels) {
	if a.ContainerAdvisor == nil {
		return
	}
	AppendContainerAdvisorText(out, a.ContainerAdvisor, labels)
}

func appendBehaviorAdvisorSection(out *[]byte, a *Analysis, labels labels) {
	if a.BehaviorAdvisor == nil {
		return
	}
	AppendBehaviorAdvisorText(out, a.BehaviorAdvisor, labels)
}

func appendFlappingPreventionSection(out *[]byte, a *Analysis, labels labels) {
	if a.FlappingPrevention == nil {
		return
	}
	AppendFlappingPreventionText(out, a.FlappingPrevention, labels)
}

func appendMetricHintsSection(out *[]byte, a *Analysis) {
	if a.MetricHints == nil || len(a.MetricHints.TroubleshootingFlows) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Metric Troubleshooting:\n"...)
	for _, flow := range a.MetricHints.TroubleshootingFlows {
		*out = fmt.Appendf(*out, "  [%s] %s (%s/%s)\n", flow.Severity, flow.Title, flow.MetricType, flow.MetricName)
		for _, step := range flow.Steps {
			*out = fmt.Appendf(*out, "    %d. %s\n", step.StepNumber, step.Description)
			if step.Command != "" {
				*out = fmt.Appendf(*out, "       $ %s\n", step.Command)
			}
		}
	}
}
