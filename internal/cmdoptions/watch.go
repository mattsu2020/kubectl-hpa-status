package cmdoptions

import "time"

// Watch mode constants select how watch and list --watch observe HPA changes.
const (
	// WatchModePoll polls the Kubernetes API at --interval.
	WatchModePoll = "poll"
	// WatchModeInformer uses a Kubernetes shared informer for event-driven updates.
	WatchModeInformer = "informer"
)

// Watch holds flags specific to watch and TUI commands.
type Watch struct {
	Watch          bool
	WatchMode      string
	WatchInterval  time.Duration
	WatchTimeout   time.Duration
	UntilCondition string
	Dashboard      bool
}
