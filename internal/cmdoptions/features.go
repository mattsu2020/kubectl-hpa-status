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

// featureSetters maps feature flag names to the bool field they control. It is
// built once and reused by Enable so the setter dispatch is O(1) and the
// cyclomatic complexity stays low regardless of how many flags exist.
var featureSetters = map[string]func(*Features, bool){
	"interpret":          func(f *Features, v bool) { f.Interpret = v },
	"noInterpret":        func(f *Features, v bool) { f.NoInterpret = v },
	"explain":            func(f *Features, v bool) { f.Explain = v },
	"suggest":            func(f *Features, v bool) { f.Suggest = v },
	"fix":                func(f *Features, v bool) { f.Fix = v },
	"recommend":          func(f *Features, v bool) { f.Recommend = v },
	"hiddenFactors":      func(f *Features, v bool) { f.HiddenFactors = v },
	"contextForAI":       func(f *Features, v bool) { f.ContextForAI = v },
	"diagnoseMetrics":    func(f *Features, v bool) { f.DiagnoseMetrics = v },
	"metricsFreshness":   func(f *Features, v bool) { f.MetricsFreshness = v },
	"metricContract":     func(f *Features, v bool) { f.MetricContract = v },
	"adapterDiagnostics": func(f *Features, v bool) { f.AdapterDiagnostics = v },
	"metricHints":        func(f *Features, v bool) { f.MetricHints = v },
	"checkResources":     func(f *Features, v bool) { f.CheckResources = v },
	"explainPods":        func(f *Features, v bool) { f.ExplainPods = v },
	"capacityContext":    func(f *Features, v bool) { f.CapacityContext = v },
	"capacityHeadroom":   func(f *Features, v bool) { f.CapacityHeadroom = v },
	"capacityDeep":       func(f *Features, v bool) { f.CapacityDeep = v },
	"capacityPlan":       func(f *Features, v bool) { f.CapacityPlan = v },
	"scalePath":          func(f *Features, v bool) { f.ScalePath = v },
	"nodeAutoscaler":     func(f *Features, v bool) { f.NodeAutoscaler = v },
	"karpenter":          func(f *Features, v bool) { f.Karpenter = v },
	"rollout":            func(f *Features, v bool) { f.Rollout = v },
	"rolloutImpact":      func(f *Features, v bool) { f.RolloutImpact = v },
	"readinessImpact":    func(f *Features, v bool) { f.ReadinessImpact = v },
	"scaleoutBlockers":   func(f *Features, v bool) { f.ScaleoutBlockers = v },
	"controllerProfile":  func(f *Features, v bool) { f.ControllerProfile = v },
	"decisionTrace":      func(f *Features, v bool) { f.DecisionTrace = v },
	"gitopsCheck":        func(f *Features, v bool) { f.GitOpsCheck = v },
	"churnDetect":        func(f *Features, v bool) { f.ChurnDetect = v },
	"flappingAdvisor":    func(f *Features, v bool) { f.FlappingAdvisor = v },
	"trendAnomaly":       func(f *Features, v bool) { f.TrendAnomaly = v },
	"containerAdvisor":   func(f *Features, v bool) { f.ContainerAdvisor = v },
	"behaviorAdvisor":    func(f *Features, v bool) { f.BehaviorAdvisor = v },
}

// Enable sets the named feature flag to true. Unknown names are ignored.
func (f *Features) Enable(name string) {
	if set, ok := featureSetters[name]; ok {
		set(f, true)
	}
}
