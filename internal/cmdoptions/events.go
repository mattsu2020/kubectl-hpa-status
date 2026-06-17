package cmdoptions

import (
	"fmt"
	"strconv"
)

// EventOption configures recent HPA event collection.
type EventOption struct {
	Enabled bool
	Limit   int
}

// Set implements pflag.Value for --events.
func (o *EventOption) Set(value string) error {
	switch value {
	case "", "true":
		o.Enabled = true
		if o.Limit <= 0 {
			o.Limit = 5
		}
		return nil
	case "false":
		o.Enabled = false
		return nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("events must be true, false, or a positive number")
	}
	if limit < 1 {
		return fmt.Errorf("events limit must be greater than zero")
	}
	o.Enabled = true
	o.Limit = limit
	return nil
}

func (o EventOption) String() string {
	if !o.Enabled {
		return "false"
	}
	return strconv.Itoa(o.Limit)
}

// Type implements pflag.Value.
func (o EventOption) Type() string {
	return "boolOrInt"
}
