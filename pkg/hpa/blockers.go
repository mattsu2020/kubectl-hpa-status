package hpa

import (
	"fmt"
	"sort"
	"strings"
)

// AnalyzeBlockers evaluates all blocker detection rules against the provided
// input and returns a BlockerReport. This is a pure function with no
// Kubernetes API dependencies.
func AnalyzeBlockers(input BlockerInput) *BlockerReport {
	hpaWantsScale := input.DesiredReplicas > input.CurrentReplicas

	rules := coreBlockerRules()
	var allFindings []BlockerFinding
	for _, rule := range rules {
		findings := rule(input)
		allFindings = append(allFindings, findings...)
	}

	allFindings = sortFindingsBySeverity(allFindings)

	report := &BlockerReport{
		HPAWantsScale:   hpaWantsScale,
		DesiredReplicas: input.DesiredReplicas,
		ReadyReplicas:   input.TargetReadyReplicas,
		Summary:         buildBlockerSummary(input, hpaWantsScale, allFindings),
		Blockers:        allFindings,
		Interpretation:  buildBlockerInterpretation(input, hpaWantsScale, allFindings),
		NextCommands:    buildBlockerNextCommands(input, allFindings),
	}

	return report
}

// sortFindingsBySeverity sorts findings HIGH > MEDIUM > INFO, preserving
// relative order within the same severity.
func sortFindingsBySeverity(findings []BlockerFinding) []BlockerFinding {
	if len(findings) == 0 {
		return findings
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return severityOrder(findings[i].Severity) < severityOrder(findings[j].Severity)
	})
	return findings
}

func severityOrder(s BlockerSeverity) int {
	switch s {
	case BlockerHigh:
		return 0
	case BlockerMedium:
		return 1
	case BlockerInfo:
		return 2
	default:
		return 3
	}
}

// buildBlockerSummary creates the one-line summary for the blocker report.
func buildBlockerSummary(input BlockerInput, hpaWantsScale bool, findings []BlockerFinding) string {
	if !hpaWantsScale {
		if hasNoActiveBlockers(findings) {
			return fmt.Sprintf("HPA has %d replicas and is not requesting scale-out. No blockers detected.", input.CurrentReplicas)
		}
		return fmt.Sprintf("HPA has %d replicas (desired=%d). Some issues detected but HPA is not actively requesting scale-out.",
			input.CurrentReplicas, input.DesiredReplicas)
	}

	gap := input.DesiredReplicas - input.TargetReadyReplicas
	if gap <= 0 {
		return fmt.Sprintf("HPA wants %d replicas and %d are Ready. Scale-out appears to be in progress.",
			input.DesiredReplicas, input.TargetReadyReplicas)
	}

	return fmt.Sprintf("HPA wants %d replicas, but only %d pods are Ready.", input.DesiredReplicas, input.TargetReadyReplicas)
}

// buildBlockerInterpretation creates a human-readable interpretation of the
// overall blocker situation.
func buildBlockerInterpretation(_ BlockerInput, hpaWantsScale bool, findings []BlockerFinding) string {
	if !hpaWantsScale {
		return "HPA is not requesting scale-out. The current replica count matches or exceeds the desired count."
	}

	cats := blockerCategoryFlags(findings)

	parts := []string{"HPA appears to be working correctly."}
	parts = appendBlockerApplicationPart(parts, cats.application)
	parts = appendBlockerSchedulingPart(parts, cats.scheduling, cats.quota)
	parts = appendBlockerReadinessPart(parts, cats.readiness)
	parts = appendBlockerNonePart(parts, cats)

	return strings.Join(parts, " ")
}

// blockerCategorySet holds booleans for each blocker category detected in findings.
type blockerCategorySet struct {
	scheduling  bool
	scaling     bool
	quota       bool
	application bool
	readiness   bool
}

// blockerCategoryFlags scans findings and returns a set of detected category flags.
func blockerCategoryFlags(findings []BlockerFinding) blockerCategorySet {
	var cats blockerCategorySet
	for _, f := range findings {
		switch f.Category {
		case "scaling":
			cats.scaling = true
		case "scheduling":
			cats.scheduling = true
		case "quota":
			cats.quota = true
		case "application":
			cats.application = true
		case "readiness":
			cats.readiness = true
		}
	}
	return cats
}

// appendBlockerApplicationPart appends the application-issue part when relevant.
func appendBlockerApplicationPart(parts []string, hasApplication bool) []string {
	if hasApplication {
		parts = append(parts, "Some pods are failing due to application or image issues (not an infrastructure problem).")
	}
	return parts
}

// appendBlockerSchedulingPart appends the scheduling/quota part based on the combination.
func appendBlockerSchedulingPart(parts []string, hasScheduling, hasQuota bool) []string {
	switch {
	case hasScheduling && hasQuota:
		parts = append(parts, "The scale-out is blocked after the HPA decision, likely by a combination of cluster capacity and namespace quota constraints.")
	case hasScheduling:
		parts = append(parts, "The scale-out is blocked after the HPA decision, likely by cluster capacity or scheduling constraints.")
	case hasQuota:
		parts = append(parts, "The scale-out may be blocked by namespace ResourceQuota limits.")
	}
	return parts
}

// appendBlockerReadinessPart appends the readiness part when relevant.
func appendBlockerReadinessPart(parts []string, hasReadiness bool) []string {
	if hasReadiness {
		parts = append(parts, "Some pods are not becoming Ready, possibly due to slow startup or misconfigured readiness probes.")
	}
	return parts
}

// appendBlockerNonePart appends a fallback part when no blockers were detected.
func appendBlockerNonePart(parts []string, cats blockerCategorySet) []string {
	if !cats.scaling && !cats.scheduling && !cats.quota && !cats.application && !cats.readiness {
		parts = append(parts, "No significant scale-out blockers were detected from visible signals.")
	}
	return parts
}

// buildBlockerNextCommands creates suggested kubectl commands for investigation.
func buildBlockerNextCommands(_ BlockerInput, findings []BlockerFinding) []string {
	seen := make(map[string]struct{})
	var commands []string

	// Add commands from findings (deduplicated).
	for _, f := range findings {
		if f.NextCommand == "" {
			continue
		}
		if _, ok := seen[f.NextCommand]; ok {
			continue
		}
		seen[f.NextCommand] = struct{}{}
		commands = append(commands, f.NextCommand)
	}

	return commands
}

// hasNoActiveBlockers returns true when all findings are INFO severity (no
// actual blockers).
func hasNoActiveBlockers(findings []BlockerFinding) bool {
	for _, f := range findings {
		if f.Severity != BlockerInfo {
			return false
		}
	}
	return true
}
