package hpa

import (
	"strings"
	"testing"
)

func TestBuildTroubleshootingFlows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hints        []MetricHint
		wantLen      int
		wantPatterns []string
		assertFlow   func(t *testing.T, flows []MetricHintTroubleshooting)
	}{
		{
			name:    "nil hints returns nil",
			hints:   nil,
			wantLen: 0,
		},
		{
			name:    "empty hints returns nil",
			hints:   []MetricHint{},
			wantLen: 0,
		},
		{
			name: "external-metric-missing produces 4 steps with commands",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "queue_depth", Pattern: "external-metric-missing", Severity: "error", Title: "External metric adapter not serving metric"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				f := flows[0]
				if f.Pattern != "external-metric-missing" {
					t.Errorf("pattern = %q, want external-metric-missing", f.Pattern)
				}
				if len(f.Steps) != 4 {
					t.Fatalf("steps = %d, want 4", len(f.Steps))
				}
				if f.Steps[0].Command == "" {
					t.Error("step 1 should have a command")
				}
				if f.Steps[0].StepNumber != 1 {
					t.Errorf("step 1 StepNumber = %d, want 1", f.Steps[0].StepNumber)
				}
				if !strings.Contains(f.Steps[0].Command, "apiservice") {
					t.Errorf("step 1 command should mention apiservice, got %q", f.Steps[0].Command)
				}
				if f.MetricType != "External" {
					t.Errorf("metricType = %q, want External", f.MetricType)
				}
				if f.MetricName != "queue_depth" {
					t.Errorf("metricName = %q, want queue_depth", f.MetricName)
				}
			},
		},
		{
			name: "external-metric-stale produces 3 steps",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "latency_ms", Pattern: "external-metric-stale", Severity: "warning", Title: "External metric data is stale"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				if len(flows[0].Steps) != 3 {
					t.Errorf("steps = %d, want 3", len(flows[0].Steps))
				}
				if flows[0].Severity != "warning" {
					t.Errorf("severity = %q, want warning", flows[0].Severity)
				}
			},
		},
		{
			name: "custom-api-service-unavailable produces 3 steps",
			hints: []MetricHint{
				{MetricType: "Pods", MetricName: "http_requests", Pattern: "custom-api-service-unavailable", Severity: "error", Title: "Custom metrics API service not available"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				if len(flows[0].Steps) != 3 {
					t.Errorf("steps = %d, want 3", len(flows[0].Steps))
				}
				if flows[0].Steps[0].DocsLink == "" {
					t.Error("step 1 should have a docs link")
				}
			},
		},
		{
			name: "external-api-service-unavailable produces 3 steps",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "sqs_depth", Pattern: "external-api-service-unavailable", Severity: "error", Title: "External metrics API service not available"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				if len(flows[0].Steps) != 3 {
					t.Errorf("steps = %d, want 3", len(flows[0].Steps))
				}
			},
		},
		{
			name: "metric-value-zero produces 3 steps",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "request_count", Pattern: "metric-value-zero", Severity: "warning", Title: "Metric reporting zero values"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				if len(flows[0].Steps) != 3 {
					t.Errorf("steps = %d, want 3", len(flows[0].Steps))
				}
			},
		},
		{
			name: "object-metric-target-not-found produces 3 steps",
			hints: []MetricHint{
				{MetricType: "Object", MetricName: "requests_per_second", Pattern: "object-metric-target-not-found", Severity: "error", Title: "Object metric target may not exist"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				if len(flows[0].Steps) != 3 {
					t.Errorf("steps = %d, want 3", len(flows[0].Steps))
				}
			},
		},
		{
			name: "missing-metric-in-status produces 3 steps",
			hints: []MetricHint{
				{MetricType: "Pods", MetricName: "http_requests", Pattern: "missing-metric-in-status", Severity: "warning", Title: "Metric has no current data in HPA status"},
			},
			wantLen: 1,
			assertFlow: func(t *testing.T, flows []MetricHintTroubleshooting) {
				t.Helper()
				if len(flows[0].Steps) != 3 {
					t.Errorf("steps = %d, want 3", len(flows[0].Steps))
				}
			},
		},
		{
			name: "unknown pattern produces no flow",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "test", Pattern: "unknown-pattern", Severity: "info", Title: "Unknown"},
			},
			wantLen: 0,
		},
		{
			name: "multiple hints produce multiple flows",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "queue_depth", Pattern: "external-metric-missing", Severity: "error", Title: "External metric adapter not serving metric"},
				{MetricType: "External", MetricName: "latency_ms", Pattern: "external-metric-stale", Severity: "warning", Title: "External metric data is stale"},
				{MetricType: "Pods", MetricName: "http_requests", Pattern: "metric-value-zero", Severity: "warning", Title: "Metric reporting zero values"},
			},
			wantLen:      3,
			wantPatterns: []string{"external-metric-missing", "external-metric-stale", "metric-value-zero"},
		},
		{
			name: "mixed known and unknown patterns",
			hints: []MetricHint{
				{MetricType: "External", MetricName: "queue_depth", Pattern: "external-metric-missing", Severity: "error", Title: "External metric adapter not serving metric"},
				{MetricType: "External", MetricName: "test", Pattern: "unknown-pattern", Severity: "info", Title: "Unknown"},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BuildTroubleshootingFlows(tt.hints)

			if tt.hints == nil || len(tt.hints) == 0 {
				if got != nil {
					t.Fatalf("expected nil for empty/nil input, got %d flows", len(got))
				}
				return
			}

			if len(got) != tt.wantLen {
				t.Fatalf("expected %d flows, got %d", tt.wantLen, len(got))
			}

			if len(tt.wantPatterns) > 0 {
				for i, wantPat := range tt.wantPatterns {
					if i >= len(got) {
						break
					}
					if got[i].Pattern != wantPat {
						t.Errorf("flow[%d].pattern = %q, want %q", i, got[i].Pattern, wantPat)
					}
				}
			}

			if tt.assertFlow != nil {
				tt.assertFlow(t, got)
			}
		})
	}
}
