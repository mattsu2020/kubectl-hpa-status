// Package lint runs static-analysis checks against an HPA manifest,
// reporting findings (severity, rule, message, recommendation) as a
// Result. It is a self-contained leaf domain depending only on
// autoscaling/v2 types. The cmd/ layer reaches it through the pkg/hpa
// re-export facade (hpaanalysis.Run, hpaanalysis.Result, etc.).
package lint

import (
	"encoding/json"
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// Severity represents the severity of a lint finding.
type Severity string

const (
	// Error indicates a critical configuration error.
	Error Severity = "ERROR"
	// Warning indicates a potential issue.
	Warning Severity = "WARN"
	// Info indicates an informational finding.
	Info Severity = "INFO"
)

// Finding represents a single lint finding.
type Finding struct {
	// Severity is the finding severity.
	Severity Severity `json:"severity" yaml:"severity"`
	// Rule is the rule ID that produced this finding.
	Rule string `json:"rule" yaml:"rule"`
	// Message is a human-readable description.
	Message string `json:"message" yaml:"message"`
	// Recommendation is an optional rationale explaining why the finding matters
	// and what the fix does.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// AutoFix is an optional proposed auto-fix for this finding.
	AutoFix *AutoFix `json:"autoFix,omitempty" yaml:"autoFix,omitempty"`
}

// Result holds the complete lint result for one or more HPA manifests.
type Result struct {
	// Findings lists all lint findings.
	Findings []Finding `json:"findings" yaml:"findings"`
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
type lintRule func(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding

// Run runs all offline lint rules against an HPA manifest and returns findings.
// This function works without cluster connection — it only inspects the manifest.
func Run(hpa *autoscalingv2.HorizontalPodAutoscaler) *Result {
	if hpa == nil {
		return &Result{
			Findings: []Finding{{
				Severity: Error,
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
		lintMinEqualsMax,
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

	var allFindings []Finding
	for _, rule := range rules {
		findings := rule(hpa)
		allFindings = append(allFindings, findings...)
	}

	result := &Result{Findings: allFindings}
	for _, f := range allFindings {
		switch f.Severity {
		case Error:
			result.Errors++
		case Warning:
			result.Warnings++
		case Info:
			result.Infos++
		}
	}
	result.Pass = result.Errors == 0
	return result
}

// lintReplicaRange checks for minReplicas > maxReplicas.
func lintReplicaRange(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	var findings []Finding

	var minReplicas int32 = 1
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	if minReplicas > hpa.Spec.MaxReplicas {
		findings = append(findings, Finding{
			Severity: Error,
			Rule:     "replica-range",
			Message:  fmt.Sprintf("minReplicas (%d) is greater than maxReplicas (%d)", minReplicas, hpa.Spec.MaxReplicas),
		})
	}

	if minReplicas > 0 && hpa.Spec.MaxReplicas/minReplicas > 10 {
		findings = append(findings, Finding{
			Severity: Warning,
			Rule:     "replica-range",
			Message: fmt.Sprintf("Wide replica range: min=%d max=%d (ratio=%d:1). Consider narrowing the range.",
				minReplicas, hpa.Spec.MaxReplicas, hpa.Spec.MaxReplicas/minReplicas),
		})
	}

	return findings
}

// lintMinGreaterThanMax checks for the critical error of min > max.
func lintMinGreaterThanMax(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	// Already covered by lintReplicaRange, but this is a separate critical check.
	if hpa.Spec.MaxReplicas == 0 {
		return []Finding{{
			Severity: Error,
			Rule:     "max-replicas-zero",
			Message:  "maxReplicas is 0, which means HPA cannot scale",
		}}
	}
	return nil
}

// lintMinEqualsMax detects when minReplicas equals maxReplicas, making the
// HPA unable to scale.
func lintMinEqualsMax(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	var minReplicas int32 = 1
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	if minReplicas == hpa.Spec.MaxReplicas {
		return []Finding{{
			Severity:       Error,
			Rule:           "min-equals-max",
			Message:        fmt.Sprintf("minReplicas and maxReplicas are both %d; HPA cannot scale", minReplicas),
			Recommendation: "Separate min and max replicas to allow the HPA to adjust pod count based on load.",
		}}
	}
	return nil
}

// lintMissingCPURequest checks if CPU resource metrics are configured but
// the manifest doesn't specify cpu requests (offline check — we can only warn).
func lintMissingCPURequest(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	var findings []Finding
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType && spec.Resource != nil {
			name := string(spec.Resource.Name)
			if name == "cpu" {
				findings = append(findings, Finding{
					Severity: Warning,
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
func lintMultiContainerResource(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	var findings []Finding
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType {
			findings = append(findings, Finding{
				Severity: Info,
				Rule:     "container-resource",
				Message: "Uses Resource metric on workload. If the target has multiple containers, " +
					"consider using ContainerResource metric to target specific containers.",
			})
			break
		}
	}
	return findings
}

// lintNoScaleDownBehavior warns when no scaleDown behavior is configured
// or when scaleDown exists but has no explicit policies.
func lintNoScaleDownBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return []Finding{{
			Severity:       Warning,
			Rule:           "behavior-scaledown",
			Message:        "No scaleDown behavior configured. The controller uses default behavior which may cause aggressive downscaling.",
			Recommendation: "Configure explicit scaleDown behavior to prevent aggressive downscaling and replica churn. See https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#default-behavior",
			AutoFix:        generateAutoFix("behavior-scaledown", hpa),
		}}
	}
	if len(hpa.Spec.Behavior.ScaleDown.Policies) == 0 {
		return []Finding{{
			Severity:       Warning,
			Rule:           "behavior-scaledown-empty",
			Message:        "scaleDown behavior exists but has no policies. The controller falls back to default behavior.",
			Recommendation: "Add explicit scaleDown policies to bound the rate of replica removal.",
		}}
	}
	return nil
}

// lintHighUtilizationTarget warns when utilization target is very high.
func lintHighUtilizationTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	var findings []Finding
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
			findings = append(findings, Finding{
				Severity:       Warning,
				Rule:           "target-utilization",
				Message:        fmt.Sprintf("%s target utilization is %d%%, which leaves little headroom for traffic bursts.", name, util),
				Recommendation: "Lower the target utilization to 70-80% to provide headroom for traffic bursts and avoid saturating pods before scaling catches up.",
				AutoFix:        generateAutoFix("target-utilization", hpa),
			})
		}
		if util < 20 {
			findings = append(findings, Finding{
				Severity: Info,
				Rule:     "target-utilization",
				Message: fmt.Sprintf("%s target utilization is %d%%, which may cause over-provisioning.",
					name, util),
			})
		}
	}
	return findings
}

// lintSingleMetric warns when only one metric is configured.
func lintSingleMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	if len(hpa.Spec.Metrics) == 1 {
		return []Finding{{
			Severity: Info,
			Rule:     "metric-coverage",
			Message:  "Only one metric configured. Consider adding a safety metric (e.g., CPU alongside a custom metric).",
		}}
	}
	return nil
}

// lintNoResourceMetrics warns when no resource metrics are configured.
func lintNoResourceMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	hasResource := false
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType || spec.Type == autoscalingv2.ContainerResourceMetricSourceType {
			hasResource = true
			break
		}
	}
	if !hasResource && len(hpa.Spec.Metrics) > 0 {
		return []Finding{{
			Severity: Info,
			Rule:     "metric-coverage",
			Message:  "No resource metrics configured. Consider adding CPU or memory as a safety signal.",
		}}
	}
	return nil
}

// lintStabilizationWindow warns about long stabilization windows.
func lintStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}
	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window != nil && *window > 900 {
		return []Finding{{
			Severity:       Warning,
			Rule:           "stabilization-window",
			Message:        fmt.Sprintf("scaleDown stabilizationWindowSeconds is %ds (>15 minutes). Scale-down may remain suppressed for a very long time.", *window),
			Recommendation: "Reduce the stabilization window to 300s (5 minutes) to allow faster recovery from traffic spikes while still preventing rapid oscillation.",
			AutoFix:        generateAutoFix("stabilization-window", hpa),
		}}
	}
	return nil
}

// lintTolerance checks tolerance values.
func lintTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	if hpa.Spec.Behavior == nil {
		return nil
	}

	var findings []Finding
	checkTol := func(name string, tol *resource.Quantity) {
		if tol == nil {
			return
		}
		val := tol.AsApproximateFloat64()
		if val < 0.01 {
			findings = append(findings, Finding{
				Severity:       Warning,
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
func lintScaleToZero(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	if hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas == 0 {
		return []Finding{{
			Severity: Info,
			Rule:     "scale-to-zero",
			Message:  "minReplicas=0 enables scale-to-zero. Ensure cold-start latency is acceptable and a reliable trigger is in place.",
		}}
	}
	return nil
}

// lintKEDAStyle warns when the HPA appears to be KEDA-managed.
func lintKEDAStyle(hpa *autoscalingv2.HorizontalPodAutoscaler) []Finding {
	if util.LooksLikeKEDAManaged(hpa) {
		return []Finding{{
			Severity: Warning,
			Rule:     "keda-managed",
			Message:  "This HPA appears to be KEDA-managed. Direct HPA patches may be overwritten by KEDA reconciliation.",
		}}
	}
	return nil
}

// FormatLintSARIF formats lint results as SARIF JSON for CI integration.
func FormatLintSARIF(result *Result, filePath string) string {
	// Collect unique rules preserving first-appearance order so the output is
	// deterministic (a map would randomize rule ordering across runs).
	var rules []map[string]any
	seenRules := make(map[string]struct{})
	for _, f := range result.Findings {
		if _, ok := seenRules[f.Rule]; ok {
			continue
		}
		seenRules[f.Rule] = struct{}{}
		rules = append(rules, map[string]any{
			"id": f.Rule,
			"shortDescription": map[string]any{
				"text": f.Rule,
			},
		})
	}

	results := make([]map[string]any, 0, len(result.Findings))
	for _, f := range result.Findings {
		level := sarifLevel(f.Severity)
		results = append(results, map[string]any{
			"ruleId": f.Rule,
			"level":  level,
			"message": map[string]any{
				"text": f.Message,
			},
			"locations": []map[string]any{
				{
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]any{
							"uri": filePath,
						},
					},
				},
			},
		})
	}

	doc := map[string]any{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]any{
			{
				"tool": map[string]any{
					"driver": map[string]any{
						"name":           "kubectl-hpa-status",
						"informationUri": "https://github.com/mattsu2020/kubectl-hpa-status",
						"rules":          rules,
					},
				},
				"results": results,
			},
		},
	}

	var sb strings.Builder
	encoder := json.NewEncoder(&sb)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		// Encoding can only fail on unsupported types; the values above are
		// all primitives/maps/slices, so this is unreachable in practice.
		// Fall back to a minimal valid SARIF document to keep the contract.
		return `{"version":"2.1.0","runs":[]}`
	}
	return strings.TrimRight(sb.String(), "\n")
}

// sarifLevel maps a Severity to a SARIF level string.
func sarifLevel(severity Severity) string {
	switch severity {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "note"
	}
}

// AutoFix describes a proposed auto-fix for a lint finding.
type AutoFix struct {
	// Patch is a JSON merge patch string that would fix the issue.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Command is a kubectl patch command to apply the fix.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Before describes the current state in human-readable form.
	Before string `json:"before,omitempty" yaml:"before,omitempty"`
	// After describes the desired state in human-readable form.
	After string `json:"after,omitempty" yaml:"after,omitempty"`
	// Risk indicates the risk level of applying this fix.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
}

// generateAutoFix produces an auto-fix proposal for a given lint rule and HPA.
// Returns nil if the rule is not auto-fixable.
func generateAutoFix(rule string, hpa *autoscalingv2.HorizontalPodAutoscaler) *AutoFix {
	switch rule {
	case "behavior-scaledown":
		return fixMissingScaleDownBehavior(hpa)
	case "target-utilization":
		return fixHighUtilizationTarget(hpa)
	case "tolerance":
		return fixTightTolerance(hpa)
	case "stabilization-window":
		return fixLongStabilizationWindow(hpa)
	default:
		return nil
	}
}

// fixMissingScaleDownBehavior generates a patch adding scaleDown behavior.
func fixMissingScaleDownBehavior(hpa *autoscalingv2.HorizontalPodAutoscaler) *AutoFix {
	patch := map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
					"policies": []map[string]any{
						{
							"type":          "Percent",
							"value":         50,
							"periodSeconds": 60,
						},
					},
				},
			},
		},
	}
	return buildAutoFix(hpa, patch, "No scaleDown behavior configured", "scaleDown with 300s stabilization + 50%/60s policy", "Low — adds guardrails to prevent aggressive downscaling")
}

// fixHighUtilizationTarget generates a patch lowering the utilization target to 80%.
func fixHighUtilizationTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) *AutoFix {
	var currentUtil int32
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType && spec.Resource != nil {
			if spec.Resource.Target.Type == autoscalingv2.UtilizationMetricType && spec.Resource.Target.AverageUtilization != nil {
				currentUtil = *spec.Resource.Target.AverageUtilization
				break
			}
		}
	}
	if currentUtil == 0 {
		return nil
	}

	patch := map[string]any{
		"spec": map[string]any{
			"metrics": []map[string]any{
				{
					"type": "Resource",
					"resource": map[string]any{
						"name": "cpu",
						"target": map[string]any{
							"type":               "Utilization",
							"averageUtilization": 80,
						},
					},
				},
			},
		},
	}

	return buildAutoFix(hpa, patch, fmt.Sprintf("%d%%", currentUtil), "80%", "Medium — changes scaling trigger point")
}

// fixTightTolerance generates a patch setting tolerance to 0.1 (10%).
func fixTightTolerance(hpa *autoscalingv2.HorizontalPodAutoscaler) *AutoFix {
	var currentVal string
	var direction string
	patch := map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{},
		},
	}

	behavior := patch["spec"].(map[string]any)["behavior"].(map[string]any)

	switch {
	case hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil:
		currentVal = fmt.Sprintf("%.2f%%", hpa.Spec.Behavior.ScaleUp.Tolerance.AsApproximateFloat64()*100)
		direction = "scaleUp"
		behavior["scaleUp"] = map[string]any{
			"tolerance": "0.1",
		}
	case hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil:
		currentVal = fmt.Sprintf("%.2f%%", hpa.Spec.Behavior.ScaleDown.Tolerance.AsApproximateFloat64()*100)
		direction = "scaleDown"
		behavior["scaleDown"] = map[string]any{
			"tolerance": "0.1",
		}
	default:
		return nil
	}

	return buildAutoFix(hpa, patch, fmt.Sprintf("%s tolerance: %s", direction, currentVal), fmt.Sprintf("%s tolerance: 0.1 (10%%)", direction), "Medium — widens the no-scale band")
}

// fixLongStabilizationWindow generates a patch reducing the window to 300s (5m).
func fixLongStabilizationWindow(hpa *autoscalingv2.HorizontalPodAutoscaler) *AutoFix {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window == nil {
		return nil
	}

	patch := map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
				},
			},
		},
	}

	return buildAutoFix(hpa, patch, fmt.Sprintf("%ds", *window), "300s (5m)", "Low — reduces cooldown delay")
}

// buildAutoFix marshals the patch and assembles an AutoFix whose Command is a
// kubectl patch suggestion (no dry-run flag, matching the historical lint
// output). Returns nil when the patch cannot be marshaled, which lets each
// caller's early-return path treat a bad patch like "no autofix available".
func buildAutoFix(hpa *autoscalingv2.HorizontalPodAutoscaler, patch map[string]any, before, after, risk string) *AutoFix {
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil
	}
	return &AutoFix{
		Patch:   string(patchJSON),
		Command: util.KubectlPatchCommandWithDryRun(hpa, string(patchJSON), util.DryRunNone),
		Before:  before,
		After:   after,
		Risk:    risk,
	}
}
