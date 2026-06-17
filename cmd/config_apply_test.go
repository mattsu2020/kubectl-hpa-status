package cmd

import "testing"

func TestApplyEventsConfigPreservesExistingPrecedence(t *testing.T) {
	limit := 10
	enabled := false
	opts := &options{
		Events: EventOption{Enabled: false, Limit: 5},
	}
	applyEventsConfig(opts, configFile{Events: &limit, EventsEnabled: &enabled}, neverChanged)
	if opts.Events.Enabled {
		t.Fatalf("expected eventsEnabled=false to keep events disabled, got enabled=%v", opts.Events.Enabled)
	}
	if opts.Events.Limit != 10 {
		t.Fatalf("expected events limit from config, got %d", opts.Events.Limit)
	}
}

func TestApplyEventsConfigSkipsExplicitFlag(t *testing.T) {
	limit := 10
	opts := &options{
		Events: EventOption{Enabled: false, Limit: 5},
	}
	applyEventsConfig(opts, configFile{Events: &limit}, alwaysChanged)
	if opts.Events.Limit != 5 {
		t.Fatalf("expected explicit events flag to keep limit 5, got %d", opts.Events.Limit)
	}
}

func TestApplyHealthScoreConfigPreservesExistingPrecedence(t *testing.T) {
	maxScore := 80
	healthScore := 60
	opts := &options{
		HealthScoreMax: -1,
	}
	applyHealthScoreConfig(opts, configFile{HealthScore: &healthScore, MaxScore: &maxScore}, neverChanged)
	// HealthScore takes precedence over MaxScore when both are set.
	if opts.HealthScoreMax != 60 {
		t.Fatalf("expected healthScoreMax=60 from HealthScore config, got %d", opts.HealthScoreMax)
	}
}

func TestApplyHealthScoreConfigSkipsExplicitAliasFlag(t *testing.T) {
	maxScore := 80
	opts := &options{
		HealthScoreMax: -1,
	}
	applyHealthScoreConfig(opts, configFile{MaxScore: &maxScore}, alwaysChanged)
	if opts.HealthScoreMax != -1 {
		t.Fatalf("expected healthScoreMax=-1 when flag is explicitly set, got %d", opts.HealthScoreMax)
	}
}

func neverChanged(string) bool {
	return false
}

func alwaysChanged(string) bool {
	return true
}
