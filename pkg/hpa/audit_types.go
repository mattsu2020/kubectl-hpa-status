package hpa

// AuditSeverity represents the severity of an audit finding.
type AuditSeverity string

const (
	// AuditCritical indicates a critical finding requiring immediate attention.
	AuditCritical AuditSeverity = "critical"
	// AuditWarning indicates a finding that warrants operator attention.
	AuditWarning AuditSeverity = "warning"
	// AuditInfo indicates an informational finding or best-practice suggestion.
	AuditInfo AuditSeverity = "info"
)

// AuditFinding represents a single best-practice audit finding.
type AuditFinding struct {
	// ID is a unique identifier for the audit rule that produced this finding.
	ID string `json:"id" yaml:"id"`
	// Title is a short description of the finding.
	Title string `json:"title" yaml:"title"`
	// Description provides detailed context about the finding.
	Description string `json:"description" yaml:"description"`
	// Severity is the severity level: critical, warning, or info.
	Severity AuditSeverity `json:"severity" yaml:"severity"`
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

// AuditProfile represents a workload profile that adjusts audit rule thresholds.
type AuditProfile string

const (
	// ProfileLatency optimizes for low-latency workloads: fast scale-up, slow scale-down.
	ProfileLatency AuditProfile = "latency"
	// ProfileCost optimizes for cost efficiency: low minReplicas, aggressive scale-down.
	ProfileCost AuditProfile = "cost"
	// ProfileBatch is for batch workloads: high CPU tolerance, no urgent scale-up.
	ProfileBatch AuditProfile = "batch"
	// ProfileKEDA is for KEDA-managed workloads: scale-to-zero, trigger/cooldown focus.
	ProfileKEDA AuditProfile = "keda"
	// ProfileCritical is for critical workloads: maxReplicas headroom, capacity checks.
	ProfileCritical AuditProfile = "critical"
)

// AuditReport holds the complete audit result for an HPA.
type AuditReport struct {
	// Namespace is the HPA namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// Score is the compliance score from 0 (worst) to 100 (fully compliant).
	Score int `json:"score" yaml:"score"`
	// Findings lists all audit findings.
	Findings []AuditFinding `json:"findings" yaml:"findings"`
	// Summary is a human-readable one-line summary of the audit.
	Summary string `json:"summary" yaml:"summary"`
	// Profile indicates the workload profile used for threshold adjustments, if any.
	Profile AuditProfile `json:"profile,omitempty" yaml:"profile,omitempty"`
}
