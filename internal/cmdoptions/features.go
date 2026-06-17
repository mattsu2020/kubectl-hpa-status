package cmdoptions

// Features groups enrichment and analysis boolean toggles for the status workflow.
// All fields are plain value-typed bool so a shallow copy produces an independent set.
type Features struct {
	// Status presentation
	Interpret     bool
	NoInterpret   bool
	Explain       bool
	Suggest       bool
	Fix           bool
	Recommend     bool
	HiddenFactors bool
	ContextForAI  bool
	// Metrics diagnostics
	DiagnoseMetrics    bool
	MetricsFreshness   bool
	MetricContract     bool
	AdapterDiagnostics bool
	MetricHints        bool
	// Pod/resource analysis
	CheckResources bool
	ExplainPods    bool
	// Capacity analysis
	CapacityContext  bool
	CapacityHeadroom bool
	CapacityDeep     bool
	CapacityPlan     bool
	ScalePath        bool
	NodeAutoscaler   bool
	Karpenter        bool
	// Rollout & blockers
	Rollout          bool
	RolloutImpact    bool
	ReadinessImpact  bool
	ScaleoutBlockers bool
	// Controller & decision
	ControllerProfile bool
	DecisionTrace     bool
	// KEDA/VPA/GitOps
	GitOpsCheck bool
	// Churn & advisors
	ChurnDetect      bool
	FlappingAdvisor  bool
	TrendAnomaly     bool
	ContainerAdvisor bool
	BehaviorAdvisor  bool
}

// Enable sets the named feature flag to true.
func (f *Features) Enable(name string) {
	switch name {
	case "interpret":
		f.Interpret = true
	case "explain":
		f.Explain = true
	case "suggest":
		f.Suggest = true
	case "diagnoseMetrics":
		f.DiagnoseMetrics = true
	case "metricsFreshness":
		f.MetricsFreshness = true
	case "metricContract":
		f.MetricContract = true
	case "adapterDiagnostics":
		f.AdapterDiagnostics = true
	case "metricHints":
		f.MetricHints = true
	case "checkResources":
		f.CheckResources = true
	case "explainPods":
		f.ExplainPods = true
	case "capacityContext":
		f.CapacityContext = true
	case "capacityHeadroom":
		f.CapacityHeadroom = true
	case "capacityDeep":
		f.CapacityDeep = true
	case "capacityPlan":
		f.CapacityPlan = true
	case "scalePath":
		f.ScalePath = true
	case "rollout":
		f.Rollout = true
	case "rolloutImpact":
		f.RolloutImpact = true
	case "readinessImpact":
		f.ReadinessImpact = true
	case "scaleoutBlockers":
		f.ScaleoutBlockers = true
	case "controllerProfile":
		f.ControllerProfile = true
	case "decisionTrace":
		f.DecisionTrace = true
	case "gitopsCheck":
		f.GitOpsCheck = true
	case "churnDetect":
		f.ChurnDetect = true
	case "flappingAdvisor":
		f.FlappingAdvisor = true
	case "trendAnomaly":
		f.TrendAnomaly = true
	case "containerAdvisor":
		f.ContainerAdvisor = true
	case "behaviorAdvisor":
		f.BehaviorAdvisor = true
	case "hiddenFactors":
		f.HiddenFactors = true
	}
}