package cmdoptions

// List holds flags specific to list and scan commands.
type List struct {
	SortBy         string
	Filter         string
	HealthScoreMin int
	HealthScoreMax int
	Problem        bool
	Summary        bool
	GitOpsDrift    bool
	Conflicts      bool
}
