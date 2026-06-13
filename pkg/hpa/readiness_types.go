package hpa

// ReadinessImpact summarizes visible pod readiness and metrics gaps that may
// make HPA controller decisions differ from status.currentMetrics.
type ReadinessImpact struct {
	LikelyAffected          bool     `json:"likelyAffected" yaml:"likelyAffected"`
	TotalPods               int32    `json:"totalPods" yaml:"totalPods"`
	NotYetReadyPods         int32    `json:"notYetReadyPods" yaml:"notYetReadyPods"`
	MissingMetricPods       int32    `json:"missingMetricPods,omitempty" yaml:"missingMetricPods,omitempty"`
	InitialReadinessDelay   string   `json:"initialReadinessDelay" yaml:"initialReadinessDelay"`
	CPUInitializationPeriod string   `json:"cpuInitializationPeriod" yaml:"cpuInitializationPeriod"`
	PossibleEffects         []string `json:"possibleEffects,omitempty" yaml:"possibleEffects,omitempty"`
	Evidence                []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	NextChecks              []string `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// ---------------------------------------------------------------------------
// Readiness Doctor types (readiness doctor command)
// ---------------------------------------------------------------------------

// ReadinessDoctorReport holds the focused readiness diagnostic for an HPA
// scale target, covering pod age distribution, probe configuration, CPU
// initialization window impact, and metric exclusion estimates.
type ReadinessDoctorReport struct {
	Namespace            string                      `json:"namespace" yaml:"namespace"`
	Name                 string                      `json:"name" yaml:"name"`
	Target               string                      `json:"target" yaml:"target"`
	Summary              string                      `json:"summary" yaml:"summary"`
	PodAgeDistribution   ReadinessPodAgeDistribution `json:"podAgeDistribution" yaml:"podAgeDistribution"`
	ProbeAnalysis        ReadinessProbeAnalysis      `json:"probeAnalysis" yaml:"probeAnalysis"`
	InitializationImpact ReadinessInitImpact         `json:"initializationImpact" yaml:"initializationImpact"`
	ExclusionEstimate    ReadinessExclusionEstimate  `json:"exclusionEstimate" yaml:"exclusionEstimate"`
	Recommendations      []string                    `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	NextChecks           []string                    `json:"nextChecks,omitempty" yaml:"nextChecks,omitempty"`
}

// ReadinessPodAgeDistribution summarizes pod age across the scale target.
type ReadinessPodAgeDistribution struct {
	TotalPods         int32 `json:"totalPods" yaml:"totalPods"`
	YoungPods         int32 `json:"youngPods" yaml:"youngPods"`
	MaturePods        int32 `json:"maturePods" yaml:"maturePods"`
	ReadyYoungPods    int32 `json:"readyYoungPods" yaml:"readyYoungPods"`
	NotReadyYoungPods int32 `json:"notReadyYoungPods" yaml:"notReadyYoungPods"`
}

// ReadinessProbeAnalysis evaluates probe configuration on the pod template.
type ReadinessProbeAnalysis struct {
	HasStartupProbe          bool     `json:"hasStartupProbe" yaml:"hasStartupProbe"`
	HasReadinessProbe        bool     `json:"hasReadinessProbe" yaml:"hasReadinessProbe"`
	ReadinessInitialDelaySec int32    `json:"readinessInitialDelaySec" yaml:"readinessInitialDelaySec"`
	StartupMaxDelaySec       int32    `json:"startupMaxDelaySec,omitempty" yaml:"startupMaxDelaySec,omitempty"`
	Assessment               string   `json:"assessment" yaml:"assessment"`
	Warnings                 []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// ReadinessInitImpact estimates how the CPU initialization window affects HPA.
type ReadinessInitImpact struct {
	CPUInitPeriodSeconds  int32  `json:"cpuInitPeriodSeconds" yaml:"cpuInitPeriodSeconds"`
	InitialReadinessDelay int32  `json:"initialReadinessDelaySeconds" yaml:"initialReadinessDelaySeconds"`
	EstimatedExcludedPods int32  `json:"estimatedExcludedPods" yaml:"estimatedExcludedPods"`
	ImpactDescription     string `json:"impactDescription" yaml:"impactDescription"`
}

// ReadinessExclusionEstimate estimates pods excluded from HPA metric calculation.
type ReadinessExclusionEstimate struct {
	NotReadyPods           int32  `json:"notReadyPods" yaml:"notReadyPods"`
	MissingMetricPods      int32  `json:"missingMetricPods" yaml:"missingMetricPods"`
	EstimatedExcludedCount int32  `json:"estimatedExcludedCount" yaml:"estimatedExcludedCount"`
	Explanation            string `json:"explanation" yaml:"explanation"`
}

// ReadinessDoctorInput is assembled by the cmd layer from Kubernetes API data.
type ReadinessDoctorInput struct {
	Namespace             string
	HPAName               string
	Target                string
	PodDetails            []ReadinessDoctorPod
	HasStartupProbe       bool
	HasReadinessProbe     bool
	ReadinessInitialDelay int32
	StartupMaxDelay       int32
	CPUInitPeriodSeconds  int32
	InitialReadinessDelay int32
	MissingMetricPods     int32
}

// ReadinessDoctorPod describes a single pod for readiness analysis.
type ReadinessDoctorPod struct {
	Name       string
	Ready      bool
	AgeSeconds int64
}
