package cmd

import "testing"

func TestApplyEventsConfigPreservesExistingPrecedence(t *testing.T) {
	limit := 10
	enabled := false
	opts := &options{
		statusOptions: statusOptions{events: eventOption{enabled: false, limit: 5}},
	}
	applyEventsConfig(opts, configFile{Events: &limit, EventsEnabled: &enabled}, neverChanged)
	if opts.events.enabled {
		t.Fatalf("expected eventsEnabled=false to keep events disabled, got enabled=%v", opts.events.enabled)
	}
	if opts.events.limit != 10 {
		t.Fatalf("expected events limit from config, got %d", opts.events.limit)
	}
}

func TestApplyEventsConfigSkipsExplicitFlag(t *testing.T) {
	limit := 10
	opts := &options{
		statusOptions: statusOptions{events: eventOption{enabled: false, limit: 5}},
	}
	applyEventsConfig(opts, configFile{Events: &limit}, alwaysChanged)
	if opts.events.limit != 5 {
		t.Fatalf("expected explicit events flag to keep limit 5, got %d", opts.events.limit)
	}
}

func TestApplyHealthScoreConfigPreservesExistingPrecedence(t *testing.T) {
	maxScore := 80
	healthScore := 60
	opts := &options{
		listOptions: listOptions{healthScoreMax: -1},
	}
	applyHealthScoreConfig(opts, configFile{HealthScore: &healthScore, MaxScore: &maxScore}, neverChanged)
	// HealthScore takes precedence over MaxScore when both are set.
	if opts.healthScoreMax != 60 {
		t.Fatalf("expected healthScoreMax=60 from HealthScore config, got %d", opts.healthScoreMax)
	}
}

func TestApplyHealthScoreConfigSkipsExplicitAliasFlag(t *testing.T) {
	maxScore := 80
	opts := &options{
		listOptions: listOptions{healthScoreMax: -1},
	}
	applyHealthScoreConfig(opts, configFile{MaxScore: &maxScore}, alwaysChanged)
	if opts.healthScoreMax != -1 {
		t.Fatalf("expected healthScoreMax=-1 when flag is explicitly set, got %d", opts.healthScoreMax)
	}
}

func neverChanged(string) bool {
	return false
}

func alwaysChanged(string) bool {
	return true
}
