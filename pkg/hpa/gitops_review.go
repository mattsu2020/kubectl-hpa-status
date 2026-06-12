package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// AnalyzeGitOpsReview compares before and after HPA manifests to detect risky
// changes. It is a pure function with no Kubernetes API dependencies beyond
// the typed HPA object.
func AnalyzeGitOpsReview(inputs []GitOpsReviewInput) *GitOpsReview {
	review := &GitOpsReview{
		Files:    []GitOpsReviewFile{},
		Findings: []GitOpsReviewFinding{},
	}

	for _, input := range inputs {
		if input.After == nil {
			continue
		}

		fileResult := GitOpsReviewFile{
			Path:    input.FilePath,
			HPAName: input.After.Name,
		}

		if input.Before != nil && input.Before.Name == input.After.Name {
			fileResult.Findings = compareHPAManifests(input.Before, input.After)
		} else {
			fileResult.Findings = reviewNewManifest(input.After)
		}

		review.Files = append(review.Files, fileResult)
		review.Findings = append(review.Findings, fileResult.Findings...)
	}

	review.RiskLevel = computeOverallRiskLevel(review.Findings)
	review.Summary = buildReviewSummary(review)
	review.Recommendation = buildReviewRecommendation(review)

	return review
}

// compareHPAManifests compares two HPA manifests for risky changes.
func compareHPAManifests(before, after *autoscalingv2.HorizontalPodAutoscaler) []GitOpsReviewFinding {
	var findings []GitOpsReviewFinding

	// maxReplicas decreased.
	if after.Spec.MaxReplicas < before.Spec.MaxReplicas {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "high",
			Category: "maxReplicas",
			Message:  fmt.Sprintf("maxReplicas decreased from %d to %d", before.Spec.MaxReplicas, after.Spec.MaxReplicas),
			Detail:   "Reducing maxReplicas may cause the HPA to cap scaling during traffic spikes, leading to degraded performance.",
		})
	}

	// maxReplicas increased significantly.
	if before.Spec.MaxReplicas > 0 && after.Spec.MaxReplicas > before.Spec.MaxReplicas*2 {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "medium",
			Category: "maxReplicas",
			Message:  fmt.Sprintf("maxReplicas increased from %d to %d (>2x)", before.Spec.MaxReplicas, after.Spec.MaxReplicas),
			Detail:   "A large maxReplicas increase may trigger unexpected node scaling. Verify capacity with `kubectl hpa-status capacity`.",
		})
	}

	// minReplicas changed.
	beforeMin := int32OrDefault(before.Spec.MinReplicas)
	afterMin := int32OrDefault(after.Spec.MinReplicas)
	if beforeMin != afterMin {
		severity := "low"
		if afterMin > beforeMin {
			severity = "medium"
		}
		findings = append(findings, GitOpsReviewFinding{
			Severity: severity,
			Category: "minReplicas",
			Message:  fmt.Sprintf("minReplicas changed from %d to %d", beforeMin, afterMin),
		})
	}

	// CPU target changed.
	beforeCPU := extractCPUTarget(before)
	afterCPU := extractCPUTarget(after)
	if beforeCPU != "" && afterCPU != "" && beforeCPU != afterCPU {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "medium",
			Category: "target",
			Message:  fmt.Sprintf("CPU target changed from %s to %s", beforeCPU, afterCPU),
		})
	}

	// Metric removed.
	if len(after.Spec.Metrics) < len(before.Spec.Metrics) {
		removed := findRemovedMetrics(before.Spec.Metrics, after.Spec.Metrics)
		if len(removed) > 0 {
			findings = append(findings, GitOpsReviewFinding{
				Severity: "medium",
				Category: "metric",
				Message:  fmt.Sprintf("metric(s) removed: %s", strings.Join(removed, ", ")),
				Detail:   "Removing metrics reduces the signals available for scaling decisions.",
			})
		}
	}

	// ScaleDown stabilization removed.
	beforeStab := extractScaleDownStabilization(before)
	afterStab := extractScaleDownStabilization(after)
	if beforeStab > 0 && afterStab == 0 {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "medium",
			Category: "stabilization",
			Message:  "scaleDown stabilization window removed",
			Detail:   "Without stabilization, the HPA may rapidly scale down and up, causing flapping.",
		})
	}

	// Behavior became more aggressive.
	if behaviorMoreAggressive(before, after) {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "low",
			Category: "behavior",
			Message:  "behavior.scaleUp policy became more aggressive",
			Detail:   "More aggressive scale-up may cause rapid pod creation that overwhelms the cluster.",
		})
	}

	return findings
}

// reviewNewManifest flags risky defaults in a new HPA manifest.
func reviewNewManifest(hpa *autoscalingv2.HorizontalPodAutoscaler) []GitOpsReviewFinding {
	var findings []GitOpsReviewFinding

	if hpa.Spec.MaxReplicas < 5 {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "low",
			Category: "maxReplicas",
			Message:  fmt.Sprintf("maxReplicas is %d (< 5); consider whether this allows sufficient headroom", hpa.Spec.MaxReplicas),
		})
	}

	cpuTarget := extractCPUTarget(hpa)
	if strings.HasSuffix(cpuTarget, "%") {
		val := strings.TrimSuffix(cpuTarget, "%")
		if val > "90" {
			findings = append(findings, GitOpsReviewFinding{
				Severity: "medium",
				Category: "target",
				Message:  fmt.Sprintf("CPU target is %s (> 90%%); HPA may scale too aggressively", cpuTarget),
			})
		}
	}

	if len(hpa.Spec.Metrics) == 0 {
		findings = append(findings, GitOpsReviewFinding{
			Severity: "high",
			Category: "metric",
			Message:  "no metrics defined; HPA cannot make scaling decisions",
		})
	}

	return findings
}

// computeOverallRiskLevel determines risk level from findings.
func computeOverallRiskLevel(findings []GitOpsReviewFinding) string {
	hasHigh := false
	hasMedium := false
	for _, f := range findings {
		switch f.Severity {
		case "high":
			hasHigh = true
		case "medium":
			hasMedium = true
		}
	}
	switch {
	case hasHigh:
		return "high"
	case hasMedium:
		return "medium"
	case len(findings) > 0:
		return "low"
	default:
		return "none"
	}
}

// buildReviewSummary creates a one-line summary.
func buildReviewSummary(review *GitOpsReview) string {
	if len(review.Findings) == 0 {
		return "no risky HPA changes detected"
	}
	highCount := 0
	medCount := 0
	lowCount := 0
	for _, f := range review.Findings {
		switch f.Severity {
		case "high":
			highCount++
		case "medium":
			medCount++
		case "low":
			lowCount++
		}
	}
	return fmt.Sprintf("%d finding(s): %d high, %d medium, %d low", len(review.Findings), highCount, medCount, lowCount)
}

// buildReviewRecommendation creates an overall recommendation.
func buildReviewRecommendation(review *GitOpsReview) string {
	switch review.RiskLevel {
	case "high":
		return "Do not merge: high-severity HPA changes require manual review."
	case "medium":
		return "Review carefully: medium-severity changes may affect scaling behavior."
	case "low":
		return "Low risk: verify the changes match the intended scaling policy."
	default:
		return "No issues found."
	}
}

// extractCPUTarget returns the CPU target utilization as a string (e.g. "70%").
func extractCPUTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType &&
			metric.Resource != nil &&
			metric.Resource.Target.AverageUtilization != nil {
			return fmt.Sprintf("%d%%", *metric.Resource.Target.AverageUtilization)
		}
	}
	return ""
}

// extractScaleDownStabilization returns the scaleDown stabilization window seconds.
func extractScaleDownStabilization(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil {
		return 0
	}
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		return 300 // Kubernetes default.
	}
	return *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
}

// behaviorMoreAggressive checks if scaleUp policy became more aggressive.
func behaviorMoreAggressive(before, after *autoscalingv2.HorizontalPodAutoscaler) bool {
	if after.Spec.Behavior == nil || after.Spec.Behavior.ScaleUp == nil {
		return false
	}
	if before.Spec.Behavior == nil || before.Spec.Behavior.ScaleUp == nil {
		return false
	}
	beforePolicies := before.Spec.Behavior.ScaleUp.Policies
	afterPolicies := after.Spec.Behavior.ScaleUp.Policies
	if len(afterPolicies) > len(beforePolicies) {
		return true
	}
	for _, ap := range afterPolicies {
		for _, bp := range beforePolicies {
			if ap.Type == bp.Type && ap.Value > bp.Value {
				return true
			}
		}
	}
	return false
}

// findRemovedMetrics identifies metrics present in before but not in after.
func findRemovedMetrics(before, after []autoscalingv2.MetricSpec) []string {
	afterSet := make(map[string]struct{}, len(after))
	for _, m := range after {
		afterSet[metricKey(m)] = struct{}{}
	}
	var removed []string
	for _, m := range before {
		key := metricKey(m)
		if _, ok := afterSet[key]; !ok {
			removed = append(removed, key)
		}
	}
	return removed
}

// metricKey returns a unique string key for a metric.
func metricKey(m autoscalingv2.MetricSpec) string {
	switch m.Type {
	case autoscalingv2.ResourceMetricSourceType:
		if m.Resource != nil {
			return fmt.Sprintf("Resource/%s", m.Resource.Name)
		}
	case autoscalingv2.ContainerResourceMetricSourceType:
		if m.ContainerResource != nil {
			return fmt.Sprintf("ContainerResource/%s/%s", m.ContainerResource.Container, m.ContainerResource.Name)
		}
	case autoscalingv2.PodsMetricSourceType:
		if m.Pods != nil {
			return fmt.Sprintf("Pods/%s", m.Pods.Metric.Name)
		}
	case autoscalingv2.ExternalMetricSourceType:
		if m.External != nil {
			return fmt.Sprintf("External/%s", m.External.Metric.Name)
		}
	}
	return string(m.Type)
}

// int32OrDefault returns the value or the default if nil.
func int32OrDefault(v *int32) int32 {
	if v == nil {
		return 1
	}
	return *v
}
