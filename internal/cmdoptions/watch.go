package cmdoptions

import "time"

// Watch holds flags specific to watch and TUI commands.
type Watch struct {
	Watch          bool
	WatchInterval  time.Duration
	WatchTimeout   time.Duration
	UntilCondition string
	Dashboard      bool
}
