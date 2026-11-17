package cmdoptions

import "time"

const (
	WatchModePoll     = "poll"
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
