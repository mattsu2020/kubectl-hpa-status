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
// (2) wire it into any command preset that should enable it in presets.go.
// Presets assign fields directly (f.CapacityDeep = true) so the compiler
// catches renames; there is deliberately no string-keyed setter registry.
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
