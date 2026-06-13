package hpa

import (
	"fmt"
	"sort"
	"strings"
)

// AnalyzeRollout produces a rollout-aware HPA diagnostics report. It detects
// rollout-related risks including missing probes, container name mismatches in
// ContainerResource metrics, and readiness instability. This is a pure function
// with no Kubernetes API dependencies.
func AnalyzeRollout(input RolloutInput) *RolloutReport {
	report := &RolloutReport{
		Namespace:         input.Namespace,
		Name:              input.HPAName,
		Target:            input.Target,
		RolloutInProgress: input.RolloutInProgress,
		Checks:            []RolloutCheck{},
		Risks:             []RolloutRisk{},
		NextActions:       []string{},
	}

	// Set new pods ready ratio.
	if input.RolloutInProgress && input.DesiredReplicas > 0 {
		report.NewPodsReady = fmt.Sprintf("%d/%d", input.UpdatedReplicas, input.DesiredReplicas)
	}

	// Run checks.
	report.Checks = runRolloutChecks(input)

	// Run risk analysis.
	report.Risks = runRolloutRisks(input)

	// Sort risks by severity: high > medium > low.
	sortRolloutRisksBySeverity(report.Risks)

	// Build summary, recommendation, and next actions.
	report.Summary = buildRolloutSummary(report, input)
	report.Recommendation = buildRolloutRecommendation(report, input)
	report.NextActions = buildRolloutNextActions(report, input)

	return report
}

// runRolloutChecks evaluates individual probe and configuration checks.
func runRolloutChecks(input RolloutInput) []RolloutCheck {
	var checks []RolloutCheck

	// startupProbe check.
	if !input.HasStartupProbe {
		checks = append(checks, RolloutCheck{
			Pass:     false,
			Category: "probe",
			Message:  "startupProbe is missing; container may take unbounded time to become ready",
		})
	} else {
		checks = append(checks, RolloutCheck{
			Pass:     true,
			Category: "probe",
			Message:  "startupProbe is configured",
		})
	}

	// readinessProbe check.
	switch {
	case !input.HasReadinessProbe:
		checks = append(checks, RolloutCheck{
			Pass:     false,
			Category: "readiness",
			Message:  "readinessProbe is missing; pod will be considered ready immediately",
		})
	case input.ReadinessInitialDelaySeconds < 5:
		checks = append(checks, RolloutCheck{
			Pass:     false,
			Category: "readiness",
			Message:  fmt.Sprintf("readinessProbe initialDelaySeconds is %d (< 5s); pod may report ready before application is initialized", input.ReadinessInitialDelaySeconds),
		})
	default:
		checks = append(checks, RolloutCheck{
			Pass:     true,
			Category: "readiness",
			Message:  "readinessProbe is configured",
		})
	}

	// Container name mismatch check.
	mismatch := findContainerMismatch(input.HPAContainerMetrics, input.NewReplicaSetContainerNames)
	if len(mismatch) > 0 {
		checks = append(checks, RolloutCheck{
			Pass:     false,
			Category: "metric",
			Message:  fmt.Sprintf("HPA ContainerResource metric references container(s) not found in new ReplicaSet: %s", strings.Join(mismatch, ", ")),
		})
	} else {
		checks = append(checks, RolloutCheck{
			Pass:     true,
			Category: "metric",
			Message:  "HPA container metrics match new ReplicaSet containers",
		})
	}

	// Rollout progress check.
	if input.RolloutInProgress {
		checks = append(checks, RolloutCheck{
			Pass:     false,
			Category: "readiness",
			Message:  fmt.Sprintf("rollout in progress; %d/%d pods ready", input.ReadyReplicas, input.DesiredReplicas),
		})
	} else {
		checks = append(checks, RolloutCheck{
			Pass:     true,
			Category: "readiness",
			Message:  "no rollout in progress",
		})
	}

	return checks
}

// runRolloutRisks evaluates risk conditions from the input.
func runRolloutRisks(input RolloutInput) []RolloutRisk {
	var risks []RolloutRisk

	// Container name mismatch is HIGH risk.
	mismatch := findContainerMismatch(input.HPAContainerMetrics, input.NewReplicaSetContainerNames)
	for _, name := range mismatch {
		risks = append(risks, RolloutRisk{
			Severity: "high",
			Category: "metric",
			Message:  fmt.Sprintf("HPA ContainerResource metric references container %q not found in new ReplicaSet", name),
			Detail:   "During a rollout, if the container name changes, HPA will fail to collect metrics from new pods and may make incorrect scaling decisions",
		})
	}

	// Pod issues during rollout.
	for _, issue := range input.PodIssues {
		severity := "medium"
		if strings.Contains(issue, "CrashLoopBackOff") || strings.Contains(issue, "ImagePullBackOff") || strings.Contains(issue, "ErrImagePull") {
			severity = "high"
		}
		risks = append(risks, RolloutRisk{
			Severity: severity,
			Category: "readiness",
			Message:  issue,
		})
	}

	return risks
}

// findContainerMismatch returns container names in metrics that are not in the
// new ReplicaSet container names.
func findContainerMismatch(metricContainers, rsContainers []string) []string {
	if len(metricContainers) == 0 {
		return nil
	}
	rsSet := make(map[string]struct{}, len(rsContainers))
	for _, name := range rsContainers {
		rsSet[name] = struct{}{}
	}
	var mismatch []string
	for _, name := range metricContainers {
		if _, ok := rsSet[name]; !ok {
			mismatch = append(mismatch, name)
		}
	}
	return mismatch
}

// sortRolloutRisksBySeverity sorts risks by severity: high > medium > low.
func sortRolloutRisksBySeverity(risks []RolloutRisk) {
	severityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(risks, func(i, j int) bool {
		return severityOrder[risks[i].Severity] < severityOrder[risks[j].Severity]
	})
}

// buildRolloutSummary creates a one-line overall assessment.
func buildRolloutSummary(report *RolloutReport, _ RolloutInput) string {
	failedCount := countFailedChecks(report.Checks)

	if failedCount > 0 {
		highCount := countRisksBySeverity(report.Risks, "high")
		mediumCount := countRisksBySeverity(report.Risks, "medium")
		var parts []string
		if highCount > 0 {
			parts = append(parts, fmt.Sprintf("%d high-severity", highCount))
		}
		if mediumCount > 0 {
			parts = append(parts, fmt.Sprintf("%d medium-severity", mediumCount))
		}
		if len(parts) > 0 {
			return fmt.Sprintf("%d check(s) failed with %s risk(s)", failedCount, strings.Join(parts, " + "))
		}
		return fmt.Sprintf("%d check(s) failed", failedCount)
	}

	if !report.RolloutInProgress {
		return "no rollout in progress; no risks detected"
	}

	return fmt.Sprintf("rollout in progress (%s); no critical risks detected", report.NewPodsReady)
}

// buildRolloutRecommendation creates an overall recommendation.
func buildRolloutRecommendation(report *RolloutReport, input RolloutInput) string {
	if len(report.Risks) == 0 && !report.RolloutInProgress {
		return "HPA scaling behavior should not be affected by rollout concerns."
	}

	var parts []string

	if report.RolloutInProgress {
		parts = append(parts, "A rollout is in progress. HPA may produce confusing metrics during the transition because old and new pod revisions coexist.")
	}

	for _, risk := range report.Risks {
		if risk.Severity == "high" && risk.Category == "metric" {
			parts = append(parts, "Container name mismatch between HPA metrics and new ReplicaSet is a critical issue that will cause HPA to fail metric collection for new pods.")
			break
		}
	}

	for _, check := range report.Checks {
		if !check.Pass && check.Category == "probe" && !input.HasStartupProbe {
			parts = append(parts, "Consider adding a startupProbe to ensure Kubernetes waits for the application to initialize before considering it started.")
			break
		}
	}

	if !input.HasReadinessProbe {
		parts = append(parts, "Ensure readinessProbe is properly configured to prevent pods from receiving traffic before they are fully initialized.")
	}

	if len(parts) == 0 {
		return "Monitor the rollout progress and verify new pods become ready as expected."
	}

	return strings.Join(parts, " ")
}

// buildRolloutNextActions creates concrete kubectl commands to run.
func buildRolloutNextActions(report *RolloutReport, input RolloutInput) []string {
	var actions []string
	seen := make(map[string]struct{})

	addAction := func(action string) {
		if _, ok := seen[action]; !ok {
			seen[action] = struct{}{}
			actions = append(actions, action)
		}
	}

	if report.RolloutInProgress {
		targetName := strings.TrimPrefix(report.Target, "Deployment/")
		targetName = strings.TrimPrefix(targetName, "StatefulSet/")
		addAction(fmt.Sprintf("kubectl rollout status deployment %s -n %s", targetName, report.Namespace))
	}

	addAction(fmt.Sprintf("kubectl get pods -n %s -l <scale-target-selector>", report.Namespace))

	if len(input.PodIssues) > 0 {
		addAction(fmt.Sprintf("kubectl describe pod -n %s -l <scale-target-selector>", report.Namespace))
		addAction(fmt.Sprintf("kubectl logs -n %s -l <scale-target-selector> --tail=50", report.Namespace))
	}

	if !input.HasStartupProbe {
		targetName := strings.TrimPrefix(report.Target, "Deployment/")
		targetName = strings.TrimPrefix(targetName, "StatefulSet/")
		addAction(fmt.Sprintf("kubectl describe deploy %s -n %s | grep -A5 Probe", targetName, report.Namespace))
	}

	return actions
}

// countFailedChecks returns the number of checks that did not pass.
func countFailedChecks(checks []RolloutCheck) int {
	count := 0
	for _, c := range checks {
		if !c.Pass {
			count++
		}
	}
	return count
}

// countRisksBySeverity counts risks with the given severity.
func countRisksBySeverity(risks []RolloutRisk, severity string) int {
	count := 0
	for _, r := range risks {
		if r.Severity == severity {
			count++
		}
	}
	return count
}
