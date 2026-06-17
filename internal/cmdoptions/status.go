package cmdoptions

// Status holds flags specific to the status and analyze workflow.
type Status struct {
	Features
	KEDA                  string
	VPA                   string
	Simulate              []string
	SimulateMetric        []string
	SimulateDuration      int32
	TargetMax             int32
	AssumeProfile         string
	ControllerProfileFile string
	Format                string
	Ask                   string
	Events                EventOption
	Report                string
	ManifestPath          string
	DecisionTraceFormat   string
	IncidentTemplate      string
	PolicyGuard           string
	PolicyGuardMode       string
	AnalysisProfile       AnalysisProfile
}
