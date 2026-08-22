package hpa

import (
	"fmt"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/healthtrend"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// This file holds the smaller section renderers that were previously inlined
// in WriteStatusTextWithOptions (text.go). Extracting them lets the
// orchestrator stay a flat list of section calls without a gocyclo exemption.

func appendHealthTrendSection(out *[]byte, a *Analysis) {
	if a.Lifecycle.HealthTrend == nil || len(a.Lifecycle.HealthTrend.Snapshots) == 0 {
		return
	}
	*out = append(*out, '\n')
	trendText := healthtrend.FormatTrendText(*a.Lifecycle.HealthTrend)
	*out = fmt.Appendf(*out, "%s\n", trendText)
}

func appendControllerProfileSection(out *[]byte, a *Analysis) {
	if a.Controllers.ControllerProfile == nil {
		return
	}
	*out = append(*out, '\n')
	appendControllerProfileText(out, a.Controllers.ControllerProfile)
}

func appendActionsSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Actions.Actions) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Actions)
	for _, action := range a.Actions.Actions {
		*out = fmt.Appendf(*out, "  - %s\n", theme.ActionLine(action))
	}
}

func appendInterpretationSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Actions.Interpretation) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Interpretation)
	for _, line := range a.Actions.Interpretation {
		*out = fmt.Appendf(*out, "  - %s\n", theme.InterpretationLine(line))
	}
}

func appendDebugSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if len(a.Lifecycle.Debug) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.Debug)
	for _, line := range a.Lifecycle.Debug {
		*out = fmt.Appendf(*out, "  - %s\n", theme.Dim.Render(line))
	}
}

func appendDecisionSignalsSection(out *[]byte, a *Analysis) {
	if len(a.Decision.DecisionSignals) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s\n", FormatDecisionSignals(a.Decision.DecisionSignals))
}

func appendDecisionTraceSection(out *[]byte, a *Analysis) {
	if a.Decision.DecisionTrace == nil {
		return
	}
	*out = append(*out, '\n')
	AppendDecisionTraceText(out, a.Decision.DecisionTrace)
}

func appendStructuredDecisionTraceSection(out *[]byte, a *Analysis, opts StatusTextOptions) {
	if a.Decision.StructuredDecisionTrace == nil {
		return
	}
	*out = append(*out, '\n')
	AppendStructuredDecisionTraceText(out, a.Decision.StructuredDecisionTrace, opts.Labels)
}

func appendAdapterDiagnosticsSection(out *[]byte, a *Analysis) {
	if a.Metrics.AdapterDiagnostics == nil {
		return
	}
	*out = append(*out, '\n')
	AppendAdapterDiagnosticsText(out, a.Metrics.AdapterDiagnostics)
}

func appendCapacityHeadroomSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.Capacity.CapacityHeadroom == nil {
		return
	}
	*out = append(*out, '\n')
	appendCapacityHeadroomText(out, a.Capacity.CapacityHeadroom, theme)
}

func appendReadinessImpactSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.Capacity.ReadinessImpact == nil {
		return
	}
	*out = append(*out, '\n')
	appendReadinessImpactText(out, a.Capacity.ReadinessImpact, theme)
}

func appendScalePathSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.Capacity.ScalePath == nil {
		return
	}
	*out = append(*out, '\n')
	appendScalePathText(out, a.Capacity.ScalePath, theme)
}

func appendRolloutDiagnosisSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.Controllers.RolloutDiagnosis == nil {
		return
	}
	*out = append(*out, '\n')
	appendRolloutDiagnosisText(out, a.Controllers.RolloutDiagnosis, theme)
}

func appendBlockerReportSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if a.Blockers.BlockerReport == nil {
		return
	}
	AppendBlockerText(out, a.Blockers.BlockerReport, theme, labels)
	appendScaleoutBlockersText(out, a.Blockers.BlockerReport, theme)
}

func appendCapacityPlanSection(out *[]byte, a *Analysis, theme style.Theme, labels labels) {
	if a.Capacity.CapacityPlan == nil {
		return
	}
	AppendCapacityPlanText(out, a.Capacity.CapacityPlan, theme, labels)
}

func appendMetricContractSection(out *[]byte, a *Analysis, theme style.Theme) {
	if a.Metrics.MetricContract == nil {
		return
	}
	*out = append(*out, '\n')
	appendMetricContractText(out, a.Metrics.MetricContract, theme)
}

func appendContainerAdvisorSection(out *[]byte, a *Analysis, labels labels) {
	if a.Advisory.ContainerAdvisor == nil {
		return
	}
	AppendContainerAdvisorText(out, a.Advisory.ContainerAdvisor, labels)
}

func appendBehaviorAdvisorSection(out *[]byte, a *Analysis, labels labels) {
	if a.Advisory.BehaviorAdvisor == nil {
		return
	}
	AppendBehaviorAdvisorText(out, a.Advisory.BehaviorAdvisor, labels)
}

func appendFlappingPreventionSection(out *[]byte, a *Analysis, labels labels) {
	if a.Stability.FlappingPrevention == nil {
		return
	}
	AppendFlappingPreventionText(out, a.Stability.FlappingPrevention, labels)
}

func appendMetricHintsSection(out *[]byte, a *Analysis) {
	if a.Metrics.MetricHints == nil || len(a.Metrics.MetricHints.TroubleshootingFlows) == 0 {
		return
	}
	*out = append(*out, '\n')
	*out = append(*out, "Metric Troubleshooting:\n"...)
	for _, flow := range a.Metrics.MetricHints.TroubleshootingFlows {
		*out = fmt.Appendf(*out, "  [%s] %s (%s/%s)\n", flow.Severity, flow.Title, flow.MetricType, flow.MetricName)
		for _, step := range flow.Steps {
			*out = fmt.Appendf(*out, "    %d. %s\n", step.StepNumber, step.Description)
			if step.Command != "" {
				*out = fmt.Appendf(*out, "       $ %s\n", step.Command)
			}
		}
	}
}
