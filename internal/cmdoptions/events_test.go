package cmdoptions

import "testing"

func TestEventOption_Set(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		wantEnabled bool
		wantLimit   int
	}{
		{name: "empty string enables with default limit", input: "", wantEnabled: true, wantLimit: 5},
		{name: "true enables with default limit", input: "true", wantEnabled: true, wantLimit: 5},
		{name: "false disables", input: "false", wantEnabled: false},
		{name: "positive number sets limit", input: "10", wantEnabled: true, wantLimit: 10},
		{name: "zero limit rejected", input: "0", wantErr: true},
		{name: "negative limit rejected", input: "-3", wantErr: true},
		{name: "non-numeric rejected", input: "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := EventOption{}
			err := o.Set(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Set(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Set(%q) unexpected error: %v", tt.input, err)
			}
			if o.Enabled != tt.wantEnabled {
				t.Fatalf("Enabled = %v, want %v", o.Enabled, tt.wantEnabled)
			}
			// Skip limit assertion when disabled (limit is irrelevant then).
			if tt.wantEnabled && o.Limit != tt.wantLimit {
				t.Fatalf("Limit = %d, want %d", o.Limit, tt.wantLimit)
			}
		})
	}
}

// TestEventOption_SetPreservesExistingLimit documents that re-enabling events
// when a positive limit is already set keeps that limit rather than resetting
// to the default (the default-5 fallback only fires when Limit <= 0).
func TestEventOption_SetPreservesExistingLimit(t *testing.T) {
	o := EventOption{Enabled: false, Limit: 9}
	if err := o.Set("true"); err != nil {
		t.Fatalf("Set(true): %v", err)
	}
	if !o.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if o.Limit != 9 {
		t.Fatalf("Limit = %d, want 9 (preserved)", o.Limit)
	}
}

func TestEventOption_String(t *testing.T) {
	tests := []struct {
		opt  EventOption
		want string
	}{
		{EventOption{Enabled: false}, "false"},
		{EventOption{Enabled: true, Limit: 5}, "5"},
		{EventOption{Enabled: true, Limit: 0}, "0"}, // enabled but zero limit edge
	}
	for _, tt := range tests {
		if got := tt.opt.String(); got != tt.want {
			t.Fatalf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestEventOption_Type(t *testing.T) {
	var o EventOption
	if got := o.Type(); got != "boolOrInt" {
		t.Fatalf("Type() = %q, want boolOrInt", got)
	}
}
