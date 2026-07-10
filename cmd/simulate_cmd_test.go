package cmd

import "testing"

func TestBuildSimulateOverridesUsesMetricTargetPath(t *testing.T) {
	overrides, err := buildSimulateOverrides([]string{"cpu=60", "queue_depth=25"}, "0.05")
	if err != nil {
		t.Fatalf("buildSimulateOverrides: %v", err)
	}
	if overrides["metric.cpu.target"] != "60" || overrides["metric.queue_depth.target"] != "25" {
		t.Fatalf("target overrides not mapped by metric name: %#v", overrides)
	}
	if overrides["tolerance"] != "0.05" {
		t.Fatalf("tolerance override missing: %#v", overrides)
	}
}

func TestParseSetMetricFlags(t *testing.T) {
	overrides, err := parseSetMetricFlags([]string{"CPU=90%", "memory=4Gi"})
	if err != nil {
		t.Fatalf("parseSetMetricFlags: %v", err)
	}
	if overrides["cpu"] != "90%" || overrides["memory"] != "4Gi" {
		t.Fatalf("unexpected metric overrides: %#v", overrides)
	}
	if _, err := parseSetMetricFlags([]string{"cpu="}); err == nil {
		t.Fatal("expected empty metric value to fail")
	}
}
