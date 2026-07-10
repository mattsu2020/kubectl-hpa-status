package cmdoptions

// List holds flags specific to list and scan commands.
type List struct {
	SortBy         string
	Filter         string
	HealthScoreMin int
	HealthScoreMax int
	// HealthScoreMaxConfigured distinguishes an explicit --health-score=0
	// from the zero value of programmatically constructed options.
	HealthScoreMaxConfigured bool
	Problem                  bool
	Summary                  bool
	GitOpsDrift              bool
	Conflicts                bool
}
