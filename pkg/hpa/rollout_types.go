package hpa

// RolloutDiagnosis summarizes rollout state that can make an HPA look broken
// even when the HPA decision itself is reasonable.
type RolloutDiagnosis struct {
	Kind                string   `json:"kind" yaml:"kind"`
	Name                string   `json:"name" yaml:"name"`
	DesiredReplicas     int32    `json:"desiredReplicas" yaml:"desiredReplicas"`
	UpdatedReplicas     int32    `json:"updatedReplicas,omitempty" yaml:"updatedReplicas,omitempty"`
	ReadyReplicas       int32    `json:"readyReplicas,omitempty" yaml:"readyReplicas,omitempty"`
	AvailableReplicas   int32    `json:"availableReplicas,omitempty" yaml:"availableReplicas,omitempty"`
	UnavailableReplicas int32    `json:"unavailableReplicas,omitempty" yaml:"unavailableReplicas,omitempty"`
	InProgress          bool     `json:"inProgress" yaml:"inProgress"`
	Reason              string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Conditions          []string `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	PodIssues           []string `json:"podIssues,omitempty" yaml:"podIssues,omitempty"`
	NextActions         []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
}

// ---------------------------------------------------------------------------
// Rollout Report types (rollout command)
// ---------------------------------------------------------------------------

// RolloutReport holds the complete rollout-aware HPA diagnostics for a
// single HPA. It detects rollout-related risks that can make HPA behavior
// confusing or incorrect during rolling updates.
type RolloutReport struct {
	// Namespace is the Kubernetes namespace of the HPA.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Name is the HPA resource name.
	Name string `json:"name" yaml:"name"`
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string `json:"target" yaml:"target"`
	// RolloutInProgress indicates whether a rollout is currently in progress.
	RolloutInProgress bool `json:"rolloutInProgress" yaml:"rolloutInProgress"`
	// NewPodsReady is the count of new pods that are ready vs total new pods.
	NewPodsReady string `json:"newPodsReady,omitempty" yaml:"newPodsReady,omitempty"`
	// Summary is a one-line overall assessment.
	Summary string `json:"summary" yaml:"summary"`
	// Checks lists individual rollout-aware check results.
	Checks []RolloutCheck `json:"checks" yaml:"checks"`
	// Risks lists detected risks during rollout.
	Risks []RolloutRisk `json:"risks,omitempty" yaml:"risks,omitempty"`
	// Recommendation is the overall recommendation text.
	Recommendation string `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	// NextActions lists concrete actions to take.
	NextActions []string `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
}

// RolloutCheck is a single rollout-aware diagnostic check result.
type RolloutCheck struct {
	// Pass is true when the check succeeds.
	Pass bool `json:"pass" yaml:"pass"`
	// Category is the check category: "probe", "metric", "readiness", "container".
	Category string `json:"category" yaml:"category"`
	// Message describes the check outcome.
	Message string `json:"message" yaml:"message"`
}

// RolloutRisk represents a detected risk during rollout that may affect HPA.
type RolloutRisk struct {
	// Severity is the risk severity: "high", "medium", "low".
	Severity string `json:"severity" yaml:"severity"`
	// Category is the risk category.
	Category string `json:"category" yaml:"category"`
	// Message describes the risk.
	Message string `json:"message" yaml:"message"`
	// Detail provides additional context.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// RolloutInput aggregates all observable signals for rollout-aware HPA
// diagnostics. The cmd layer assembles this from Kubernetes API calls.
type RolloutInput struct {
	// Namespace is the Kubernetes namespace.
	Namespace string
	// HPAName is the HPA resource name.
	HPAName string
	// Target is the scaleTargetRef in "Kind/Name" format.
	Target string
	// RolloutInProgress indicates whether a rollout is currently in progress.
	RolloutInProgress bool
	// UpdatedReplicas is the count of pods running the updated revision.
	UpdatedReplicas int32
	// ReadyReplicas is the count of ready pods.
	ReadyReplicas int32
	// DesiredReplicas is the desired replica count from the workload.
	DesiredReplicas int32
	// HasStartupProbe indicates whether the pod template has a startupProbe.
	HasStartupProbe bool
	// HasReadinessProbe indicates whether the pod template has a readinessProbe.
	HasReadinessProbe bool
	// ReadinessInitialDelaySeconds is the readinessProbe initialDelaySeconds.
	ReadinessInitialDelaySeconds int32
	// HPAContainerMetrics lists container names referenced by HPA
	// ContainerResource metrics.
	HPAContainerMetrics []string
	// TemplateContainerNames lists container names from the current pod template.
	TemplateContainerNames []string
	// NewReplicaSetContainerNames lists container names from the new ReplicaSet.
	NewReplicaSetContainerNames []string
	// PodIssues lists pod-level issues detected during rollout.
	PodIssues []string
}
