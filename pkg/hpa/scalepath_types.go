package hpa

// ScalePath describes the visible scale-up path from the HPA recommendation
// through the workload, ReplicaSets, pods, and scheduler-facing signals.
type ScalePath struct {
	Steps            []ScalePathStep         `json:"steps" yaml:"steps"`
	BlockingPoint    string                  `json:"blockingPoint,omitempty" yaml:"blockingPoint,omitempty"`
	Evidence         []string                `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	NextActions      []string                `json:"nextActions,omitempty" yaml:"nextActions,omitempty"`
	ProbeWarnings    []string                `json:"probeWarnings,omitempty" yaml:"probeWarnings,omitempty"`
	SchedulerInfo    *ScalePathSchedulerInfo `json:"schedulerInfo,omitempty" yaml:"schedulerInfo,omitempty"`
	QuotaChecks      []ScalePathQuotaCheck   `json:"quotaChecks,omitempty" yaml:"quotaChecks,omitempty"`
	AutoscalerEvents []string                `json:"autoscalerEvents,omitempty" yaml:"autoscalerEvents,omitempty"`
}

// ScalePathStep is one hop in the HPA-to-pod scaling path.
type ScalePathStep struct {
	Name    string `json:"name" yaml:"name"`
	Summary string `json:"summary" yaml:"summary"`
}

// ScalePathTarget is the observed HPA scale target.
type ScalePathTarget struct {
	Kind            string
	Name            string
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// ScalePathReplicaSet is a ReplicaSet participating in the target path.
type ScalePathReplicaSet struct {
	Name            string
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// ScalePathPod is the pod-level state used by scale path analysis.
type ScalePathPod struct {
	Name          string
	Phase         string
	Ready         bool
	Unschedulable bool
	Reasons       []string
}

// ProbeInfo describes a probe (readiness or startup) on the pod template.
type ProbeInfo struct {
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty" yaml:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int32 `json:"periodSeconds,omitempty" yaml:"periodSeconds,omitempty"`
	TimeoutSeconds      int32 `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	FailureThreshold    int32 `json:"failureThreshold,omitempty" yaml:"failureThreshold,omitempty"`
	SuccessThreshold    int32 `json:"successThreshold,omitempty" yaml:"successThreshold,omitempty"`
}

// ScalePathPodTemplate captures the pod template configuration relevant to
// scale-path analysis (probes, scheduling constraints).
type ScalePathPodTemplate struct {
	ReadinessProbe  *ProbeInfo        `json:"readinessProbe,omitempty" yaml:"readinessProbe,omitempty"`
	StartupProbe    *ProbeInfo        `json:"startupProbe,omitempty" yaml:"startupProbe,omitempty"`
	NodeSelector    map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
	Tolerations     []string          `json:"tolerations,omitempty" yaml:"tolerations,omitempty"`
	AffinitySummary string            `json:"affinitySummary,omitempty" yaml:"affinitySummary,omitempty"`
	TopologySpread  []string          `json:"topologySpread,omitempty" yaml:"topologySpread,omitempty"`
}

// ScalePathSchedulerInfo describes scheduling constraints that may affect
// pod placement during scale-up.
type ScalePathSchedulerInfo struct {
	TaintConflicts            []string `json:"taintConflicts,omitempty" yaml:"taintConflicts,omitempty"`
	NodeSelectorLabels        int      `json:"nodeSelectorLabels,omitempty" yaml:"nodeSelectorLabels,omitempty"`
	AffinityConstraints       []string `json:"affinityConstraints,omitempty" yaml:"affinityConstraints,omitempty"`
	TopologySpreadConstraints []string `json:"topologySpreadConstraints,omitempty" yaml:"topologySpreadConstraints,omitempty"`
	Warning                   string   `json:"warning,omitempty" yaml:"warning,omitempty"`
}

// ScalePathQuotaCheck describes a ResourceQuota that may block scale-up.
type ScalePathQuotaCheck struct {
	Name     string `json:"name" yaml:"name"`
	Resource string `json:"resource" yaml:"resource"`
	Used     string `json:"used" yaml:"used"`
	Hard     string `json:"hard" yaml:"hard"`
	Blocking bool   `json:"blocking" yaml:"blocking"`
}

// ScalePathInput contains the observable Kubernetes API signals used to build
// a scale path. It intentionally excludes controller-internal calculations.
type ScalePathInput struct {
	Target           *ScalePathTarget
	ReplicaSets      []ScalePathReplicaSet
	Pods             []ScalePathPod
	Events           []Event
	PodTemplate      *ScalePathPodTemplate
	ResourceQuotas   []ScalePathQuotaCheck
	AutoscalerEvents []string
	NotReadyPods     []ScalePathPod
}

// PendingPodInfo describes a pending pod and its scheduling constraints.
type PendingPodInfo struct {
	Name          string   `json:"name" yaml:"name"`
	Phase         string   `json:"phase" yaml:"phase"`
	Unschedulable bool     `json:"unschedulable" yaml:"unschedulable"`
	Reasons       []string `json:"reasons,omitempty" yaml:"reasons,omitempty"`
}

// QuotaConstraint describes a ResourceQuota that limits the scale target.
type QuotaConstraint struct {
	Name     string `json:"name" yaml:"name"`
	Resource string `json:"resource" yaml:"resource"`
	Used     string `json:"used" yaml:"used"`
	Hard     string `json:"hard" yaml:"hard"`
	Message  string `json:"message" yaml:"message"`
}

// PDBInterference describes a PodDisruptionBudget that may interfere with scaling.
type PDBInterference struct {
	Name           string `json:"name" yaml:"name"`
	MinAvailable   string `json:"minAvailable,omitempty" yaml:"minAvailable,omitempty"`
	MaxUnavailable string `json:"maxUnavailable,omitempty" yaml:"maxUnavailable,omitempty"`
	Disruption     string `json:"disruption" yaml:"disruption"`
}
