// Package audit runs best-practice configuration audits against an HPA,
// producing a scored Report with actionable findings. It is a self-contained
// domain depending only on autoscaling/v2 types plus the shared
// pkg/hpa/internal/{util,conditions} helpers. The cmd/ layer reaches it
// through the pkg/hpa re-export facade (hpaanalysis.AuditHPA, etc.). The
// *_text.go renderer stays in pkg/hpa because it shares the labels machinery.
package audit

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/util"
)

// healthScoreMax is the starting audit score; findings deduct from it.
// Mirrors pkg/hpa.healthScoreMax (unexported there). Keep in sync.
const healthScoreMax = 100

// Severity represents the severity of an audit finding.
type Severity string

const (
	// AuditCritical indicates a critical finding requiring immediate attention.
	AuditCritical Severity = "critical"
	// AuditWarning indicates a finding that warrants operator attention.
	AuditWarning Severity = "warning"
	// AuditInfo indicates an informational finding or best-practice suggestion.
	AuditInfo Severity = "info"
)

// Finding represents a single best-practice audit finding.
type Finding struct {
	// ID is a unique identifier for the audit rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Title is a short description of the finding.
	Title string `json:"title" yaml:"title"`
	// Description provides detailed context about the finding.
	Description string `json:"description" yaml:"description"`
	// Severity is the severity level: critical, warning, or info.
	Severity Severity `json:"severity" yaml:"severity"`
	// Category groups related findings (e.g. "stabilization", "replica-range").
	Category string `json:"category" yaml:"category"`
	// Current shows the current configuration value.
	Current string `json:"current,omitempty" yaml:"current,omitempty"`
	// Recommended shows the recommended configuration value.
	Recommended string `json:"recommended,omitempty" yaml:"recommended,omitempty"`
	// Patch is a JSON merge patch to fix the finding, if applicable.
	Patch string `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Command is the kubectl command to apply the patch.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Risk indicates the risk level of applying the patch.
	Risk string `json:"risk,omitempty" yaml:"risk,omitempty"`
	// References lists URLs or docs for further reading.
	References []string `json:"references,omitempty" yaml:"references,omitempty"`
}

// Profile represents a workload profile that adjusts audit rule thresholds.
type Profile string

const (
	// ProfileLatency optimizes for low-latency workloads: fast scale-up, slow scale-down.
	ProfileLatency Profile = "latency"
	// ProfileCost optimizes for cost efficiency: low minReplicas, aggressive scale-down.
	ProfileCost Profile = "cost"
	// ProfileBatch is for batch workloads: high CPU tolerance, no urgent scale-up.
	ProfileBatch Profile = "batch"
	// ProfileKEDA is for KEDA-managed workloads: scale-to-zero, trigger/cooldown focus.
	ProfileKEDA Profile = "keda"
	// ProfileCritical is for critical workloads: maxReplicas headroom, capacity checks.
	ProfileCritical Profile = "critical"
)

// Report holds the complete audit result for an HPA.
type Report struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Score is the compliance score from 0 (worst) to 100 (fully compliant).
	Score int `json:"score" yaml:"score"`
	// Findings lists all audit findings.
	Findings []Finding `json:"findings" yaml:"findings"`
	// Summary is a human-readable one-line summary of the audit.
	Summary string `json:"summary" yaml:"summary"`
	// Profile indicates the workload profile used for threshold adjustments, if any.
	Profile Profile `json:"profile,omitempty" yaml:"profile,omitempty"`
}

// Rule examines an HPA for best-practice compliance and returns findings.
type Rule func(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding

// Run runs all audit rules against the HPA and returns a compliance report.
func Run(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) *Report {
	return RunWithProfile(hpa, minReplicas, "")
}

// RunWithProfile runs all audit rules against the HPA using the given
// workload profile to adjust thresholds. An empty profile uses defaults.
func RunWithProfile(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32, profile Profile) *Report {
	if hpa == nil {
		return &Report{Score: 0, Summary: "HPA is nil"}
	}

	report := &Report{
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Target:    fmt.Sprintf("%s/%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name),
		Score:     healthScoreMax, // start at 100
		Profile:   profile,
	}

	for _, rule := range coreRulesWithProfile(profile) {
		findings := rule(hpa, minReplicas)
		report.Findings = append(report.Findings, findings...)
	}

	// Calculate score based on findings
	for _, f := range report.Findings {
		switch f.Severity {
		case AuditCritical:
			report.Score -= 20
		case AuditWarning:
			report.Score -= 10
			// AuditInfo: no deduction
		}
	}
	if report.Score < 0 {
		report.Score = 0
	}

	report.Summary = buildAuditSummary(report)
	return report
}

// coreRules returns the ordered list of best-practice audit rules.
func coreRules() []Rule {
	return coreRulesWithProfile("")
}

// coreRulesWithProfile returns the base audit rules plus any
// profile-specific rules that apply for the given workload profile.
func coreRulesWithProfile(profile Profile) []Rule {
	base := []Rule{
		stabilizationWindowRule,
		replicaRangeRule,
		behaviorPolicyRule,
		metricCoverageRule,
		toleranceRule,
		scaleToZeroRule,
		resourceRequestRule,
		kedaRule,
		targetUtilizationRule,
	}

	profileRules := profileSpecificRules(profile)
	return append(base, profileRules...)
}

func buildAuditSummary(report *Report) string {
	critical := 0
	warning := 0
	info := 0
	for _, f := range report.Findings {
		switch f.Severity {
		case AuditCritical:
			critical++
		case AuditWarning:
			warning++
		case AuditInfo:
			info++
		}
	}
	if len(report.Findings) == 0 {
		return "No best-practice issues found."
	}
	return fmt.Sprintf("Found %d critical, %d warnings, %d informational findings (score: %d/100)", critical, warning, info, report.Score)
}

// stabilizationWindowRule checks whether the scale-down stabilization window
// is explicitly configured. When unset, the controller-manager default (300s)
// applies implicitly, which can surprise operators across Kubernetes upgrades.
func stabilizationWindowRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior != nil && hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
		return nil
	}

	patch := util.MarshalJSON(map[string]any{
		"spec": map[string]any{
			"behavior": map[string]any{
				"scaleDown": map[string]any{
					"stabilizationWindowSeconds": 300,
				},
			},
		},
	})

	return []Finding{
		{
			ID:          "stabilization-window",
			Title:       "Stabilization window not explicitly configured",
			Description: "scaleDown.stabilizationWindowSeconds is unset. The controller-manager default (300s) applies implicitly. Explicit configuration prevents surprise behavior changes across Kubernetes upgrades.",
			Severity:    AuditWarning,
			Category:    "stabilization",
			Current:     "unset (default 300s)",
			Recommended: "Set stabilizationWindowSeconds explicitly",
			Patch:       patch,
			Command:     util.KubectlPatchCommand(hpa, patch),
			Risk:        "low",
		},
	}
}

// replicaRangeRule checks for wide replica ranges and unset minReplicas.
func replicaRangeRule(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	var findings []Finding

	if minReplicas > 0 && hpa.Spec.MaxReplicas/minReplicas > 10 {
		findings = append(findings, Finding{
			ID:          "replica-range",
			Title:       "Wide replica range may indicate instability",
			Description: fmt.Sprintf("maxReplicas/minReplicas ratio is %d (>10x). A wide range can cause large, abrupt scaling events. Consider narrowing the range or adding stepped scaling policies.", hpa.Spec.MaxReplicas/minReplicas),
			Severity:    AuditWarning,
			Category:    "replica-range",
			Current:     fmt.Sprintf("min=%d max=%d (ratio=%d:1)", minReplicas, hpa.Spec.MaxReplicas, hpa.Spec.MaxReplicas/minReplicas),
			Recommended: "Narrow the range to 10x or less, or add explicit scaling policies",
		})
	}

	if hpa.Spec.MinReplicas == nil {
		findings = append(findings, Finding{
			ID:          "replica-range",
			Title:       "minReplicas uses default value",
			Description: "spec.minReplicas is nil, so the Kubernetes default of 1 applies. Explicit configuration makes the intent clear and avoids depending on controller defaults.",
			Severity:    AuditInfo,
			Category:    "replica-range",
			Current:     "unset (default 1)",
			Recommended: "Set minReplicas explicitly",
		})
	}

	return findings
}

// behaviorPolicyRule checks for missing explicit scaleUp and scaleDown policies.
func behaviorPolicyRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	if util.MissingPolicies(hpa.Spec.Behavior, "scaleUp") {
		findings = append(findings, Finding{
			ID:          "behavior-policy",
			Title:       "No explicit scaleUp policies configured",
			Description: "Without explicit scaleUp policies, the HPA controller uses default behavior which may not match your workload's scaling needs. Adding policies makes burst response predictable.",
			Severity:    AuditInfo,
			Category:    "behavior",
			Current:     "default scaleUp behavior",
			Recommended: "Add explicit scaleUp policies with bounded rates",
		})
	}

	if util.MissingPolicies(hpa.Spec.Behavior, "scaleDown") {
		findings = append(findings, Finding{
			ID:          "behavior-policy",
			Title:       "No explicit scaleDown policies configured",
			Description: "Without explicit scaleDown policies, the HPA controller uses default behavior which may cause aggressive downscaling. Adding policies keeps downscale predictable.",
			Severity:    AuditInfo,
			Category:    "behavior",
			Current:     "default scaleDown behavior",
			Recommended: "Add explicit scaleDown policies with bounded rates",
		})
	}

	return findings
}

// metricCoverageRule checks for single-metric configurations and missing
// resource metrics.
func metricCoverageRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	if len(hpa.Spec.Metrics) == 0 {
		return findings
	}

	if len(hpa.Spec.Metrics) == 1 {
		findings = append(findings, Finding{
			ID:          "metric-coverage",
			Title:       "Single metric configured",
			Description: "Only one metric is configured. A single signal can be noisy or delayed. Consider adding a safety metric (e.g., CPU alongside a queue-depth metric) to improve scaling reliability.",
			Severity:    AuditInfo,
			Category:    "metrics",
			Current:     fmt.Sprintf("1 metric (%s)", metricTypeName(hpa.Spec.Metrics[0])),
			Recommended: "Add a complementary safety metric",
		})
	}

	hasResource := false
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType || spec.Type == autoscalingv2.ContainerResourceMetricSourceType {
			hasResource = true
			break
		}
	}

	if !hasResource {
		findings = append(findings, Finding{
			ID:          "metric-coverage",
			Title:       "No resource metrics configured",
			Description: "All configured metrics are external, custom, pods, or object types. Consider adding CPU or memory as a safety signal; resource metrics are reliably available and can catch scenarios where business metrics are delayed.",
			Severity:    AuditInfo,
			Category:    "metrics",
			Current:     "no resource metrics",
			Recommended: "Add CPU or memory as a safety metric",
		})
	}

	return findings
}

// toleranceRule checks whether the tolerance is explicitly configured.
func toleranceRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil {
		return []Finding{{
			ID:          "tolerance",
			Title:       "Tolerance uses default value",
			Description: "Tolerance is not explicitly set. The default 0.1 (10%) applies. Workloads with tight scaling requirements may benefit from a narrower tolerance for faster response.",
			Severity:    AuditInfo,
			Category:    "tolerance",
			Current:     "default (0.1 / 10%)",
			Recommended: "Consider setting an explicit tolerance if the default is too loose",
		}}
	}

	hasExplicitTolerance := (hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil) ||
		(hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil)

	if hasExplicitTolerance {
		return nil
	}

	return []Finding{{
		ID:          "tolerance",
		Title:       "Tolerance uses default value",
		Description: "Tolerance uses the default 0.1 (10%). Workloads with tight scaling requirements may benefit from a narrower tolerance.",
		Severity:    AuditInfo,
		Category:    "tolerance",
		Current:     "default (0.1 / 10%)",
		Recommended: "Consider setting an explicit tolerance if the default is too loose",
	}}
}

// scaleToZeroRule warns when scale-to-zero is enabled.
func scaleToZeroRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas != 0 {
		return nil
	}

	return []Finding{
		{
			ID:          "scale-to-zero",
			Title:       "Scale-to-zero enabled",
			Description: "minReplicas is set to 0, enabling scale-to-zero. Cold start latency may impact availability when traffic resumes. Ensure your workload can tolerate the cold-start delay and that a reliable trigger mechanism is in place.",
			Severity:    AuditWarning,
			Category:    "scale-to-zero",
			Current:     "minReplicas=0",
			Recommended: "Verify cold-start latency is acceptable and triggers are reliable",
		},
	}
}

// resourceRequestRule notes that Resource metrics require corresponding
// resource requests on the pod containers.
func resourceRequestRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType {
			continue
		}
		name := string(spec.Resource.Name)
		findings = append(findings, Finding{
			ID:          "resource-requests",
			Title:       fmt.Sprintf("Verify %s resource requests", name),
			Description: fmt.Sprintf("Resource metric %q is configured. HPA utilization calculations depend on container resource requests being set correctly. Missing or zero requests produce misleading utilization percentages.", name),
			Severity:    AuditInfo,
			Category:    "resources",
			Current:     fmt.Sprintf("resource metric %s configured", name),
			Recommended: fmt.Sprintf("Ensure container %s requests are set on the scale target pods", name),
		})
	}

	return findings
}

// kedaRule warns when an HPA appears to be KEDA-managed.
func kedaRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if !util.LooksLikeKEDAManaged(hpa) {
		return nil
	}

	return []Finding{
		{
			ID:          "keda-managed",
			Title:       "HPA appears KEDA-managed",
			Description: "This HPA appears to be managed by KEDA. Direct HPA patches may be overwritten by KEDA reconciliation. Changes should be made on the owning ScaledObject instead.",
			Severity:    AuditInfo,
			Category:    "keda",
			Current:     "KEDA-managed HPA detected",
			Recommended: "Modify the ScaledObject rather than patching the HPA directly",
		},
	}
}

// targetUtilizationRule checks for extreme target utilization values.
func targetUtilizationRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	var findings []Finding

	for _, spec := range hpa.Spec.Metrics {
		if spec.Type != autoscalingv2.ResourceMetricSourceType || spec.Resource == nil {
			continue
		}

		target := spec.Resource.Target
		if target.Type != autoscalingv2.UtilizationMetricType || target.AverageUtilization == nil {
			continue
		}

		utilization := *target.AverageUtilization
		name := string(spec.Resource.Name)

		if utilization > 90 {
			findings = append(findings, Finding{
				ID:          "target-utilization",
				Title:       fmt.Sprintf("High %s target utilization (>90%%)", name),
				Description: fmt.Sprintf("%s target utilization is set to %d%%, which leaves little headroom for traffic bursts. Consider lowering to 70-80%% for production workloads.", name, utilization),
				Severity:    AuditWarning,
				Category:    "target-utilization",
				Current:     fmt.Sprintf("%d%%", utilization),
				Recommended: "70-80% for production workloads",
			})
		} else if utilization < 30 {
			findings = append(findings, Finding{
				ID:          "target-utilization",
				Title:       fmt.Sprintf("Low %s target utilization (<30%%)", name),
				Description: fmt.Sprintf("%s target utilization is set to %d%%, which may cause over-provisioning and unnecessary resource costs. Verify this low threshold is intentional.", name, utilization),
				Severity:    AuditInfo,
				Category:    "target-utilization",
				Current:     fmt.Sprintf("%d%%", utilization),
				Recommended: "Typical range is 50-80%; verify this is intentional",
			})
		}
	}

	return findings
}

// metricTypeName returns a short display name for a metric spec type.
func metricTypeName(spec autoscalingv2.MetricSpec) string {
	switch spec.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if spec.Resource != nil {
			return fmt.Sprintf("Resource/%s", spec.Resource.Name)
		}
		return "Resource"
	case autoscalingv2.ContainerResourceMetricSourceType:
		if spec.ContainerResource != nil {
			return fmt.Sprintf("ContainerResource/%s", spec.ContainerResource.Name)
		}
		return "ContainerResource"
	case autoscalingv2.ExternalMetricSourceType:
		if spec.External != nil {
			return fmt.Sprintf("External/%s", spec.External.Metric.Name)
		}
		return "External"
	case autoscalingv2.PodsMetricSourceType:
		if spec.Pods != nil {
			return fmt.Sprintf("Pods/%s", spec.Pods.Metric.Name)
		}
		return "Pods"
	case autoscalingv2.ObjectMetricSourceType:
		if spec.Object != nil {
			return fmt.Sprintf("Object/%s", spec.Object.Metric.Name)
		}
		return "Object"
	default:
		return string(spec.Type)
	}
}

// ---------------------------------------------------------------------------
// Profile-specific audit rules
// ---------------------------------------------------------------------------

// profileSpecificRules returns audit rules that apply only when a specific
// workload profile is selected. Each rule applies profile-adjusted thresholds.
func profileSpecificRules(profile Profile) []Rule {
	switch profile {
	case ProfileLatency:
		return []Rule{
			latencyStabilizationRule,
			latencyScaleUpPolicyRule,
		}
	case ProfileCost:
		return []Rule{
			costMinReplicasRule,
			costScaleDownRule,
		}
	case ProfileBatch:
		return []Rule{
			batchToleranceRule,
		}
	case ProfileKEDA:
		return []Rule{
			kedaScaleToZeroRule,
			kedaCooldownRule,
		}
	case ProfileCritical:
		return []Rule{
			criticalMaxHeadroomRule,
			criticalMinReplicasRule,
		}
	default:
		return nil
	}
}

// latencyStabilizationRule warns when the scale-up stabilization window is
// too long for a latency-sensitive workload.
func latencyStabilizationRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleUp == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
	if window == nil || *window <= 60 {
		return nil
	}

	return []Finding{
		{
			ID:          "latency-stabilization",
			Title:       "Scale-up stabilization window too long for latency profile",
			Description: fmt.Sprintf("scaleUp.stabilizationWindowSeconds is %ds. For latency-sensitive workloads, scale-up should respond within 60s. A long stabilization window delays adding capacity under load.", *window),
			Severity:    AuditWarning,
			Category:    "profile-latency",
			Current:     fmt.Sprintf("%ds", *window),
			Recommended: "≤60s for latency-sensitive workloads",
		},
	}
}

// latencyScaleUpPolicyRule warns when no scaleUp policy is configured or when
// the policy period is too long for latency-sensitive workloads.
func latencyScaleUpPolicyRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if util.MissingPolicies(hpa.Spec.Behavior, "scaleUp") {
		return []Finding{
			{
				ID:          "latency-scale-up-policy",
				Title:       "No scaleUp policy for latency-sensitive workload",
				Description: "Latency-sensitive workloads need explicit scaleUp policies to ensure fast, predictable scaling. Without policies, default behavior may be too conservative.",
				Severity:    AuditWarning,
				Category:    "profile-latency",
				Current:     "default scaleUp behavior",
				Recommended: "Add scaleUp policy with PeriodSeconds ≤ 30",
			},
		}
	}

	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleUp == nil {
		return nil
	}

	for _, p := range hpa.Spec.Behavior.ScaleUp.Policies {
		if p.PeriodSeconds > 30 {
			return []Finding{
				{
					ID:          "latency-scale-up-period",
					Title:       "ScaleUp policy period too long for latency profile",
					Description: fmt.Sprintf("A scaleUp policy has PeriodSeconds=%d. For latency-sensitive workloads, scaling should react within 30s windows.", p.PeriodSeconds),
					Severity:    AuditInfo,
					Category:    "profile-latency",
					Current:     fmt.Sprintf("PeriodSeconds=%d", p.PeriodSeconds),
					Recommended: "PeriodSeconds ≤ 30",
				},
			}
		}
	}

	return nil
}

// costMinReplicasRule warns when minReplicas is higher than necessary for a
// cost-optimized workload.
func costMinReplicasRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas <= 2 {
		return nil
	}

	return []Finding{
		{
			ID:          "cost-min-replicas",
			Title:       "minReplicas higher than recommended for cost profile",
			Description: fmt.Sprintf("minReplicas is %d. For cost-optimized workloads, consider lowering to 1-2 to allow aggressive scale-down during low traffic.", minReplicas),
			Severity:    AuditInfo,
			Category:    "profile-cost",
			Current:     fmt.Sprintf("minReplicas=%d", minReplicas),
			Recommended: "1-2 for cost-optimized workloads",
		},
	}
}

// costScaleDownRule warns when scale-down is too slow for cost optimization.
func costScaleDownRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window != nil && *window > 120 {
		return []Finding{
			{
				ID:          "cost-scaledown-window",
				Title:       "Scale-down stabilization window too long for cost profile",
				Description: fmt.Sprintf("scaleDown.stabilizationWindowSeconds is %ds. For cost-optimized workloads, consider reducing to ≤120s to release idle capacity faster.", *window),
				Severity:    AuditInfo,
				Category:    "profile-cost",
				Current:     fmt.Sprintf("%ds", *window),
				Recommended: "≤120s for cost-optimized workloads",
			},
		}
	}

	return nil
}

// batchToleranceRule warns when tolerance is too tight for batch workloads.
func batchToleranceRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	// Default HPA tolerance is 0.1 (10%).
	toleranceValue := 0.1
	if hpa.Spec.Behavior != nil {
		if hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.Tolerance != nil {
			toleranceValue = hpa.Spec.Behavior.ScaleUp.Tolerance.AsApproximateFloat64()
		} else if hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.Tolerance != nil {
			toleranceValue = hpa.Spec.Behavior.ScaleDown.Tolerance.AsApproximateFloat64()
		}
	}

	if toleranceValue < 0.3 {
		return []Finding{
			{
				ID:          "batch-tolerance",
				Title:       "Tolerance too tight for batch workload",
				Description: fmt.Sprintf("Tolerance is %.0f%%. Batch workloads should avoid reacting to small metric fluctuations. Consider increasing tolerance to ≥30%% to reduce unnecessary scaling.", toleranceValue*100),
				Severity:    AuditInfo,
				Category:    "profile-batch",
				Current:     fmt.Sprintf("%.0f%%", toleranceValue*100),
				Recommended: "≥30% for batch workloads",
			},
		}
	}

	return nil
}

// kedaScaleToZeroRule recommends scale-to-zero for KEDA-managed workloads
// that still have minReplicas > 0.
func kedaScaleToZeroRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas == 0 {
		return nil
	}

	return []Finding{
		{
			ID:          "keda-scale-to-zero",
			Title:       "KEDA workload not configured for scale-to-zero",
			Description: fmt.Sprintf("minReplicas is %d. KEDA-managed workloads with reliable triggers can benefit from scale-to-zero (minReplicas=0) to save costs during idle periods.", minReplicas),
			Severity:    AuditInfo,
			Category:    "profile-keda",
			Current:     fmt.Sprintf("minReplicas=%d", minReplicas),
			Recommended: "minReplicas=0 with a reliable KEDA trigger",
		},
	}
}

// kedaCooldownRule warns when scale-down stabilization is too long for
// KEDA workloads that should respond quickly to trigger changes.
func kedaCooldownRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return nil
	}

	window := hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if window != nil && *window > 300 {
		return []Finding{
			{
				ID:          "keda-cooldown",
				Title:       "Scale-down stabilization too long for KEDA profile",
				Description: fmt.Sprintf("scaleDown.stabilizationWindowSeconds is %ds. KEDA workloads with active triggers should scale down promptly when the trigger signal decreases. Consider reducing to ≤300s.", *window),
				Severity:    AuditInfo,
				Category:    "profile-keda",
				Current:     fmt.Sprintf("%ds", *window),
				Recommended: "≤300s for KEDA-managed workloads",
			},
		}
	}

	return nil
}

// criticalMaxHeadroomRule warns when there is insufficient headroom between
// current and maxReplicas for critical workloads.
func criticalMaxHeadroomRule(hpa *autoscalingv2.HorizontalPodAutoscaler, _ int32) []Finding {
	current := hpa.Status.CurrentReplicas
	maxRep := hpa.Spec.MaxReplicas

	if current == 0 || maxRep == 0 {
		return nil
	}

	headroom := maxRep - current
	headroomPercent := float64(headroom) / float64(current) * 100

	if headroomPercent < 50 {
		return []Finding{
			{
				ID:          "critical-max-headroom",
				Title:       "Insufficient maxReplicas headroom for critical workload",
				Description: fmt.Sprintf("Current replicas=%d, maxReplicas=%d (%.0f%% headroom). Critical workloads need ≥50%% headroom to absorb sudden traffic spikes. Consider raising maxReplicas.", current, maxRep, headroomPercent),
				Severity:    AuditWarning,
				Category:    "profile-critical",
				Current:     fmt.Sprintf("%d/%d (%.0f%% headroom)", current, maxRep, headroomPercent),
				Recommended: "≥50% headroom for critical workloads",
			},
		}
	}

	return nil
}

// criticalMinReplicasRule warns when minReplicas is below the minimum
// recommended for critical workloads.
func criticalMinReplicasRule(_ *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []Finding {
	if minReplicas >= 2 {
		return nil
	}

	return []Finding{
		{
			ID:          "critical-min-replicas",
			Title:       "minReplicas too low for critical workload",
			Description: fmt.Sprintf("minReplicas is %d. Critical workloads should maintain at least 2 replicas for high availability during node failures or pod evictions.", minReplicas),
			Severity:    AuditWarning,
			Category:    "profile-critical",
			Current:     fmt.Sprintf("minReplicas=%d", minReplicas),
			Recommended: "≥2 for critical workloads",
		},
	}
}
