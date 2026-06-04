package cmd

import "testing"

func TestApplyEventsConfigPreservesExistingPrecedence(t *testing.T) {
	limit := 10
	enabled := false
	opts := &options{events: eventOption{enabled: false, limit: 5}}

	applyEventsConfig(opts, configFile{Events: &limit, EventsEnabled: &enabled}, neverChanged)

	if opts.events.enabled {
		t.Fatalf("expected eventsEnabled to override events enabled state")
	}
	if opts.events.limit != 10 {
		t.Fatalf("expected events limit from config, got %d", opts.events.limit)
	}
}

func TestApplyEventsConfigSkipsExplicitFlag(t *testing.T) {
	limit := 10
	opts := &options{events: eventOption{enabled: false, limit: 5}}

	applyEventsConfig(opts, configFile{Events: &limit}, alwaysChanged)

	if opts.events.enabled {
		t.Fatalf("expected explicit events flag to keep enabled state")
	}
	if opts.events.limit != 5 {
		t.Fatalf("expected explicit events flag to keep limit, got %d", opts.events.limit)
	}
}

func TestApplyHealthScoreConfigPreservesExistingPrecedence(t *testing.T) {
	maxScore := 80
	healthScore := 60
	opts := &options{healthScoreMax: -1}

	applyHealthScoreConfig(opts, configFile{MaxScore: &maxScore, HealthScore: &healthScore}, neverChanged)

	if opts.healthScoreMax != 60 {
		t.Fatalf("expected healthScore to override maxScore, got %d", opts.healthScoreMax)
	}
}

func TestApplyHealthScoreConfigSkipsExplicitAliasFlag(t *testing.T) {
	maxScore := 80
	opts := &options{healthScoreMax: -1}

	applyHealthScoreConfig(opts, configFile{MaxScore: &maxScore}, func(name string) bool {
		return name == "health-score"
	})

	if opts.healthScoreMax != -1 {
		t.Fatalf("expected explicit health-score flag to keep max score, got %d", opts.healthScoreMax)
	}
}

func neverChanged(string) bool {
	return false
}

func alwaysChanged(string) bool {
	return true
}
