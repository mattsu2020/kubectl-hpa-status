// Package blocker detects scale-out blockers (pending pods, unschedulable
// pods, quota limits, readiness stalls, node capacity) that prevent an HPA
// from achieving its desired replica count. It is a self-contained leaf
// domain depending only on standard library types. The cmd/ layer reaches
// it through the pkg/hpa re-export facade (hpaanalysis.AnalyzeBlockers,
// hpaanalysis.Report, etc.). The blocker_text.go renderer stays in
// pkg/hpa because it shares the labels machinery.
package blocker

import (
	"fmt"
	"sort"
	"strings"
)

// Severity classifies how significantly a finding blocks scale-out.
type Severity string

const (
	// BlockerHigh indicates a definite scale-out blocker requiring immediate attention.
	BlockerHigh Severity = "HIGH"
	// BlockerMedium indicates a likely blocker that warrants investigation.
	BlockerMedium Severity = "MEDIUM"
	// BlockerInfo indicates an informational finding with no blocking effect.
	BlockerInfo Severity = "INFO"
)

// Finding represents a single detected scale-out blocker.
type Finding struct {
	// ID is a unique identifier for the detection rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Severity is the blocker severity: HIGH, MEDIUM, or INFO.
	Severity Severity `json:"severity" yaml:"severity"`
	// Category groups related findings: "scheduling", "quota", "application", "readiness", "info".
	Category string `json:"category" yaml:"category"`
	// Message is a human-readable description of the blocker.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context about the blocker.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	// NextCommand suggests a kubectl command to investigate further.
	NextCommand string `json:"nextCommand,omitempty" yaml:"nextCommand,omitempty"`
}

// Report holds the complete scale-out blocker analysis for an HPA.
type Report struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// HPAWantsScale is true when desiredReplicas > currentReplicas.
	HPAWantsScale bool `json:"hpaWantsScale" yaml:"hpaWantsScale"`
	// DesiredReplicas is the desired replica count from HPA status.
	DesiredReplicas int32 `json:"desiredReplicas" yaml:"desiredReplicas"`
	// ReadyReplicas is the count of ready pods on the scale target.
	ReadyReplicas int32 `json:"readyReplicas" yaml:"readyReplicas"`
	// Summary is a one-line summary of the blocker analysis.
	Summary string `json:"summary" yaml:"summary"`
	// Blockers lists all detected blocker findings sorted by severity.
	Blockers []Finding `json:"blockers" yaml:"blockers"`
	// Interpretation is a human-readable explanation of the overall situation.
	Interpretation string `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	// NextCommands lists suggested kubectl commands for further investigation.
	NextCommands []string `json:"nextCommands" yaml:"nextCommands"`
}

// ContainerStatusSummary holds container-level status for blocker detection.
type ContainerStatusSummary struct {
	// Pod is the pod name.
	Pod string `json:"pod" yaml:"pod"`
	// Container is the container name.
	Container string `json:"container" yaml:"container"`
	// Waiting is true when the container is in a waiting state.
	Waiting bool `json:"waiting" yaml:"waiting"`
	// WaitingReason is the reason for the waiting state (e.g. ImagePullBackOff, CrashLoopBackOff).
	WaitingReason string `json:"waitingReason,omitempty" yaml:"waitingReason,omitempty"`
	// RestartCount is the number of container restarts.
	RestartCount int32 `json:"restartCount" yaml:"restartCount"`
}

// NodeCapacitySummary holds node-level capacity information for deep analysis.
type NodeCapacitySummary struct {
	// TotalNodes is the total number of nodes in the cluster.
	TotalNodes int32 `json:"totalNodes" yaml:"totalNodes"`
	// AllocCPU is the sum of allocatable CPU across all nodes.
	AllocCPU string `json:"allocatableCpu,omitempty" yaml:"allocatableCpu,omitempty"`
	// AllocMemory is the sum of allocatable memory across all nodes.
	AllocMemory string `json:"allocatableMemory,omitempty" yaml:"allocatableMemory,omitempty"`
	// TaintedNodes is the count of nodes with at least one taint that has NoSchedule or NoExecute effect.
	TaintedNodes int32 `json:"taintedNodes,omitempty" yaml:"taintedNodes,omitempty"`
	// Hints provides actionable hints based on node capacity analysis.
	Hints []string `json:"hints,omitempty" yaml:"hints,omitempty"`
}

// Input aggregates all observable signals for scale-out blocker analysis.
// The cmd layer assembles this from multiple kube fetchers, keeping the core
// analysis in pkg/hpa free of Kubernetes API dependencies.
type Input struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string
	// DesiredReplicas is the HPA desired replica count.
	DesiredReplicas int32
	// CurrentReplicas is the HPA current replica count.
	CurrentReplicas int32
	// MinReplicas is the HPA minimum replica count.
	MinReplicas int32
	// MaxReplicas is the HPA maximum replica count.
	MaxReplicas int32
	// TargetReadyReplicas is the ready replica count from the scale target.
	TargetReadyReplicas int32
	// TargetDesiredReplicas is the desired replica count from the scale target.
	TargetDesiredReplicas int32
	// PendingPods lists pods in Pending phase with scheduling details.
	PendingPods []PodInfo
	// ReadyPods is the count of pods in Running/Ready state.
	ReadyPods int32
	// TotalPods is the total number of pods for the scale target.
	TotalPods int32
	// ContainerStatuses holds container-level status for failure detection.
	ContainerStatuses []ContainerStatusSummary
	// FailedSchedulingEvents lists events with reason FailedScheduling.
	FailedSchedulingEvents []string
	// Quotas lists ResourceQuota constraints near their limits.
	Quotas []QuotaInfo
	// NodeCapacity holds node-level capacity (only populated with --capacity-deep).
	NodeCapacity *NodeCapacitySummary
	// ScalingActive indicates whether the HPA ScalingActive condition is True.
	ScalingActive bool
}

// PodInfo holds pod-level information relevant to blocker detection.
type PodInfo struct {
	// Name is the pod name.
	Name string
	// Phase is the pod phase (Pending, Running, etc.).
	Phase string
	// Unschedulable is true when the pod has an unschedulable condition.
	Unschedulable bool
	// Reasons lists scheduling failure reasons from pod conditions.
	Reasons []string
}

// QuotaInfo holds ResourceQuota usage information for blocker detection.
type QuotaInfo struct {
	// Name is the ResourceQuota name.
	Name string
	// Resource is the resource name (e.g. requests.cpu, requests.memory).
	Resource string
	// Used is the current usage value as a string.
	Used string
	// Hard is the hard limit as a string.
	Hard string
	// Ratio is the usage ratio (used/hard), 0 if hard is zero.
	Ratio float64
}

// AnalyzeBlockers evaluates all blocker rules against the given input and
// returns a Report. This is a pure function with no Kubernetes API
// dependencies.
func AnalyzeBlockers(input Input) *Report {
	hpaWantsScale := input.DesiredReplicas > input.CurrentReplicas

	rules := coreBlockerRules()
	var allFindings []Finding
	for _, rule := range rules {
		findings := rule(input)
		allFindings = append(allFindings, findings...)
	}

	allFindings = sortFindingsBySeverity(allFindings)

	report := &Report{
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
func sortFindingsBySeverity(findings []Finding) []Finding {
	if len(findings) == 0 {
		return findings
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return severityOrder(findings[i].Severity) < severityOrder(findings[j].Severity)
	})
	return findings
}

func severityOrder(s Severity) int {
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
func buildBlockerSummary(input Input, hpaWantsScale bool, findings []Finding) string {
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
func buildBlockerInterpretation(_ Input, hpaWantsScale bool, findings []Finding) string {
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
func blockerCategoryFlags(findings []Finding) blockerCategorySet {
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
func buildBlockerNextCommands(_ Input, findings []Finding) []string {
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
func hasNoActiveBlockers(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity != BlockerInfo {
			return false
		}
	}
	return true
}

type blockerRule func(input Input) []Finding

// coreBlockerRules returns the standard set of blocker detection rules,
// ordered from highest to lowest typical severity.
func coreBlockerRules() []blockerRule {
	return []blockerRule{
		scaleOutDesiredRule,
		pendingPodsRule,
		unschedulableRule,
		containerFailureRule,
		quotaNearLimitRule,
		readinessStalledRule,
		nodeCapacityRule,
		metricsHealthyInfoRule,
	}
}
