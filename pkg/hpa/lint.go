package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// LintSeverity represents the severity of a lint finding.
type LintSeverity string

const (
	// LintError indicates a critical configuration error.
	LintError LintSeverity = "ERROR"
	// LintWarning indicates a potential issue.
	LintWarning LintSeverity = "WARN"
	// LintInfo indicates an informational finding.
	LintInfo LintSeverity = "INFO"
)

// LintFinding represents a single lint finding.
type LintFinding struct {
	// Severity is the finding severity.
	Severity LintSeverity `json:"severity" yaml:"severity"`
	// Rule is the rule ID that produced this finding.
	Rule string `json:"rule" yaml:"rule"`
	// Message is a human-readable description.
	Message string `json:"message" yaml:"message"`
	// Recommendation is an optional rationale explaining why the finding matters
	// and what the fix does.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// AutoFix is an optional proposed auto-fix for this finding.
	AutoFix *LintAutoFix `json:"autoFix,omitempty" yaml:"autoFix,omitempty"`
}

// LintResult holds the complete lint result for one or more HPA manifests.
type LintResult struct {
	// Findings lists all lint findings.
	Findings []LintFinding `json:"findings" yaml:"findings"`
	// Errors is the count of ERROR-level findings.
	Errors int `json:"errors" yaml:"errors"`
	// Warnings is the count of WARN-level findings.
	Warnings int `json:"warnings" yaml:"warnings"`
	// Infos is the count of INFO-level findings.
	Infos int `json:"infos" yaml:"infos"`
	// Pass is true when there are no ERROR-level findings.
	Pass bool `json:"pass" yaml:"pass"`
}

// lintRule is a pure function that examines an HPA manifest and returns findings.
type lintRule func(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding

// LintHPA runs all offline lint rules against an HPA manifest and returns findings.
// This function works without cluster connection — it only inspects the manifest.
func LintHPA(hpa *autoscalingv2.HorizontalPodAutoscaler) *LintResult {
	if hpa == nil {
		return &LintResult{
			Findings: []LintFinding{{
				Severity: LintError,
				Rule:     "nil-hpa",
				Message:  "HPA manifest is nil or empty",
			}},
			Errors: 1,
			Pass:   false,
		}
	}

	rules := []lintRule{
		lintReplicaRange,
		lintMinGreaterThanMax,
		lintMissingCPURequest,
		lintMultiContainerResource,
		lintNoScaleDownBehavior,
		lintHighUtilizationTarget,
		lintSingleMetric,
		lintNoResourceMetrics,
		lintStabilizationWindow,
		lintTolerance,
		lintScaleToZero,
		lintKEDAStyle,
	}

	var allFindings []LintFinding
	for _, rule := range rules {
		findings := rule(hpa)
		allFindings = append(allFindings, findings...)
	}

	result := &LintResult{Findings: allFindings}
	for _, f := range allFindings {
		switch f.Severity {
		case LintError:
			result.Errors++
		case LintWarning:
			result.Warnings++
		case LintInfo:
			result.Infos++
		}
	}
	result.Pass = result.Errors == 0
	return result
}

// lintReplicaRange checks for minReplicas > maxReplicas.
func lintReplicaRange(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	var findings []LintFinding

	var minReplicas int32 = 1
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	if minReplicas > hpa.Spec.MaxReplicas {
		findings = append(findings, LintFinding{
			Severity: LintError,
			Rule:     "replica-range",
			Message:  fmt.Sprintf("minReplicas (%d) is greater than maxReplicas (%d)", minReplicas, hpa.Spec.MaxReplicas),
		})
	}

	if minReplicas > 0 && hpa.Spec.MaxReplicas/minReplicas > 10 {
		findings = append(findings, LintFinding{
			Severity: LintWarning,
			Rule:     "replica-range",
			Message: fmt.Sprintf("Wide replica range: min=%d max=%d (ratio=%d:1). Consider narrowing the range.",
				minReplicas, hpa.Spec.MaxReplicas, hpa.Spec.MaxReplicas/minReplicas),
		})
	}

	return findings
}

// lintMinGreaterThanMax checks for the critical error of min > max.
func lintMinGreaterThanMax(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	// Already covered by lintReplicaRange, but this is a separate critical check.
	if hpa.Spec.MaxReplicas == 0 {
		return []LintFinding{{
			Severity: LintError,
			Rule:     "max-replicas-zero",
			Message:  "maxReplicas is 0, which means HPA cannot scale",
		}}
	}
	return nil
}

// lintMissingCPURequest checks if CPU resource metrics are configured but
// the manifest doesn't specify cpu requests (offline check — we can only warn).
func lintMissingCPURequest(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	var findings []LintFinding
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType && spec.Resource != nil {
			name := string(spec.Resource.Name)
			if name == "cpu" {
				findings = append(findings, LintFinding{
					Severity: LintWarning,
					Rule:     "resource-requests",
					Message: "HPA uses CPU utilization but target container cpu requests cannot be verified offline. " +
						"Ensure containers have cpu requests set; missing requests produce misleading utilization percentages.",
				})
			}
		}
	}
	return findings
}

// lintMultiContainerResource warns when Resource metrics are used on what appears
// to be a multi-container workload (heuristic: we can't know for sure offline).
func lintMultiContainerResource(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	var findings []LintFinding
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType {
			findings = append(findings, LintFinding{
				Severity: LintInfo,
				Rule:     "container-resource",
				Message: "Uses Resource metric on workload. If the target has multiple containers, " +
					"consider using ContainerResource metric to target specific containers.",
			})
			break
		}
	}
	return findings
}

// lintNoScaleDownBehavior warns when no scaleDown behavior is configured.
func lintNoScaleDownBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return []LintFinding{{
			Severity:       LintWarning,
			Rule:           "behavior-scaledown",
			Message:        "No scaleDown behavior configured. The controller uses default behavior which may cause aggressive downscaling.",
			Recommendation: "Configure explicit scaleDown behavior to prevent aggressive downscaling and replica churn. See https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#default-behavior",
			AutoFix:        generateAutoFix("behavior-scaledown", hpa),
		}}
	}
	return nil
}

// lintHighUtilizationTarget warns when utilization target is very high.
func lintHighUtilizationTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	var findings []LintFinding
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType || spec.Resource == nil {
			continue
		}
		target := spec.Resource.Target
		if target.Type != autoscalingv2.UtilizationMetricType || target.AverageUtilization == nil {
			continue
		}
		util := *target.AverageUtilization
		name := string(spec.Resource.Name)
		if util > 90 {
			findings = append(findings, LintFinding{
				Severity:       LintWarning,
				Rule:           "target-utilization",
				Message:        fmt.Sprintf("%s target utilization is %d%%, which leaves little headroom for traffic bursts.", name, util),
				Recommendation: "Lower the target utilization to 70-80% to provide headroom for traffic bursts and avoid saturating pods before scaling catches up.",
				AutoFix:        generateAutoFix("target-utilization", hpa),
			})
		}
		if util < 20 {
			findings = append(findings, LintFinding{
				Severity: LintInfo,
				Rule:     "target-utilization",
				Message: fmt.Sprintf("%s target utilization is %d%%, which may cause over-provisioning.",
					name, util),
			})
		}
	}
	return findings
}

// lintSingleMetric warns when only one metric is configured.
func lintSingleMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	if len(hpa.Spec.Metrics) == 1 {
		return []LintFinding{{
			Severity: LintInfo,
			Rule:     "metric-coverage",
			Message:  "Only one metric configured. Consider adding a safety metric (e.g., CPU alongside a custom metric).",
		}}
	}
	return nil
}

// lintNoResourceMetrics warns when no resource metrics are configured.
func lintNoResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	hasResource := false
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType || spec.Type == autoscalingv2.ContainerResourceMetricSourceType {
			hasResource = true
			break
		}
	}
	if !hasResource && len(hpa.Spec.Metrics) > 0 {
		return []LintFinding{{
			Severity: LintInfo,
			Rule:     "metric-coverage",
			Message:  "No resource metrics configured. Consider adding CPU or memory as a safety signal.",
		}}
	}
	return nil
}

// lintStabilizationWindow warns about long stabilization windows.
func lintStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}
	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window != nil && *window > 900 {
		return []LintFinding{{
			Severity:       LintWarning,
			Rule:           "stabilization-window",
			Message:        fmt.Sprintf("scaleDown stabilizationWindowSeconds is %ds (>15 minutes). Scale-down may remain suppressed for a very long time.", *window),
			Recommendation: "Reduce the stabilization window to 300s (5 minutes) to allow faster recovery from traffic spikes while still preventing rapid oscillation.",
			AutoFix:        generateAutoFix("stabilization-window", hpa),
		}}
	}
	return nil
}

// lintTolerance checks tolerance values.
func lintTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	if hpa.Spec.Behavior == nil {
		return nil
	}

	var findings []LintFinding
	checkTol := func(name string, tol *resource.Quantity) {
		if tol == nil {
			return
		}
		val := tol.AsApproximateFloat64()
		if val < 0.01 {
			findings = append(findings, LintFinding{
				Severity:       LintWarning,
				Rule:           "tolerance",
				Message:        fmt.Sprintf("%s tolerance is %.2f%%, which is very tight. This may cause frequent scaling oscillations.", name, val*100),
				Recommendation: "Increase tolerance to 0.1 (10%) to reduce scaling noise. A tight tolerance causes the HPA to react to minor metric fluctuations.",
				AutoFix:        generateAutoFix("tolerance", hpa),
			})
		}
	}

	if hpa.Spec.Behavior.ScaleUp != nil {
		checkTol("scaleUp", hpa.Spec.Behavior.ScaleUp.Tolerance)
	}
	if hpa.Spec.Behavior.ScaleDown != nil {
		checkTol("scaleDown", hpa.Spec.Behavior.ScaleDown.Tolerance)
	}

	return findings
}

// lintScaleToZero warns about scale-to-zero configurations.
func lintScaleToZero(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	if hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas == 0 {
		return []LintFinding{{
			Severity: LintInfo,
			Rule:     "scale-to-zero",
			Message:  "minReplicas=0 enables scale-to-zero. Ensure cold-start latency is acceptable and a reliable trigger is in place.",
		}}
	}
	return nil
}

// lintKEDAStyle warns when the HPA appears to be KEDA-managed.
func lintKEDAStyle(hpa *autoscalingv2.HorizontalPodAutoscaler) []LintFinding {
	if looksLikeKEDAManaged(hpa) {
		return []LintFinding{{
			Severity: LintWarning,
			Rule:     "keda-managed",
			Message:  "This HPA appears to be KEDA-managed. Direct HPA patches may be overwritten by KEDA reconciliation.",
		}}
	}
	return nil
}

// FormatLintSARIF formats lint results as SARIF JSON for CI integration.
func FormatLintSARIF(result *LintResult, filePath string) string {
	var sb strings.Builder
	sb.WriteString(`{
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "kubectl-hpa-status",
          "informationUri": "https://github.com/mattsu2020/kubectl-hpa-status",
          "rules": [
`)

	// Collect unique rules.
	ruleSet := make(map[string]struct{})
	for _, f := range result.Findings {
		ruleSet[f.Rule] = struct{}{}
	}
	first := true
	for rule := range ruleSet {
		if !first {
			sb.WriteString(",\n")
		}
		first = false
		sb.WriteString(fmt.Sprintf(`            {
              "id": "%s",
              "shortDescription": { "text": "%s" }
            }`, rule, rule))
	}

	sb.WriteString(`
          ]
        }
      },
      "results": [
`)

	for i, f := range result.Findings {
		if i > 0 {
			sb.WriteString(",\n")
		}
		level := "note"
		switch f.Severity {
		case LintError:
			level = "error"
		case LintWarning:
			level = "warning"
		}
		sb.WriteString(fmt.Sprintf(`        {
          "ruleId": "%s",
          "level": "%s",
          "message": { "text": "%s" },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": { "uri": "%s" }
              }
            }
          ]
        }`, f.Rule, level, escapeJSON(f.Message), escapeJSON(filePath)))
	}

	sb.WriteString(`
      ]
    }
  ]
}`)
	return sb.String()
}

// escapeJSON escapes a string for JSON embedding.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
