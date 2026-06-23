package cmdoptions

// Features groups enrichment and analysis boolean toggles for the status
// workflow. Fields are organized into logical domains via comment sections; a
// full sub-struct split was considered but rejected because every flag is
// accessed as f.<Name> throughout cmd/ and pkg/hpa/, and grouping behind
// sub-structs would force a pervasive rename for no behavioral gain. All
// fields are plain value-typed bool so a shallow copy produces an independent
// set.
//
// When adding a new flag: (1) place it in the matching domain section below,
// (2) register it in featureSetters, (3) wire it into any command preset that
// should enable it in presets.go.
type Features struct {
	// --- Presentation: controls which human-facing sections are rendered. ---
	Interpret     bool
	NoInterpret   bool
	Explain       bool
	Suggest       bool
	Fix           bool
	Recommend     bool
	HiddenFactors bool
	ContextForAI  bool

	// --- Status depth tiers: coarse switches that enable groups of enrichers
	// at once, so users do not need to know every individual flag.
	//   --deep    turns on capacity/rollout/adapter-diagnostics analysis;
	//   --no-enrich disables all enrichment (HPA-only, RBAC-light output).
	// Both are convenience aggregators; the individual flags remain available.
	Deep     bool
	NoEnrich bool
	HPAOnly  bool

	// --- Metrics diagnostics: inspect the metrics pipeline health. ---
	DiagnoseMetrics    bool
	MetricsFreshness   bool
	MetricContract     bool
	AdapterDiagnostics bool
	MetricHints        bool

	// --- Pod/resource analysis: workload-level inspection. ---
	CheckResources bool
	ExplainPods    bool

	// --- Capacity analysis: cluster headroom and scale-out feasibility. ---
	CapacityContext  bool
	CapacityHeadroom bool
	CapacityDeep     bool
	CapacityPlan     bool
	ScalePath        bool
	NodeAutoscaler   bool
	Karpenter        bool

	// --- Rollout & blockers: deployment progress and scale-out gates. ---
	Rollout          bool
	RolloutImpact    bool
	ReadinessImpact  bool
	ScaleoutBlockers bool

	// --- Controller & decision: HPA controller timing and decision trace. ---
	ControllerProfile bool
	DecisionTrace     bool

	// --- KEDA/VPA/GitOps: enrichment integrations. ---
	GitOpsCheck bool

	// --- Churn & advisors: thrashing detection and tuning advisors. ---
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
	"deep":               func(f *Features, v bool) { f.Deep = v },
	"noEnrich":           func(f *Features, v bool) { f.NoEnrich = v },
	"hpaOnly":            func(f *Features, v bool) { f.HPAOnly = v },
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
