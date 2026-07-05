package keda

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// TestAnalyze_NilInputs confirms the nil guards on the canonical Analyze entry
// point (exercised through the pkg/hpa facade as AnalyzeKEDA in keda_test.go,
// but kept here too so the leaf package is independently testable).
func TestAnalyze_NilInputs(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if got := Analyze(hpa, nil); got != nil {
		t.Fatalf("Analyze(hpa, nil) = %v, want nil", got)
	}
	if got := Analyze(nil, &Analysis{}); got != nil {
		t.Fatalf("Analyze(nil, keda) = %v, want nil", got)
	}
}

func TestAnalyze_ReplicaBoundMismatch(t *testing.T) {
	minRepl := int32(2)
	maxRepl := int32(20)
	idleRepl := int32(0)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minRepl,
			MaxReplicas: 10,
		},
	}
	k := &Analysis{
		MinReplicaCount:  &minRepl,
		MaxReplicaCount:  &maxRepl,
		IdleReplicaCount: &idleRepl,
	}
	lines := Analyze(hpa, k)
	joined := strings.Join(lines, "\n")
	// Min matches so no min line; max differs so we expect the max mismatch line.
	if strings.Contains(joined, "minReplicaCount=2 differs") {
		t.Errorf("did not expect min mismatch line when values match:\n%s", joined)
	}
	if !strings.Contains(joined, "maxReplicaCount=20 differs from HPA maxReplicas=10") {
		t.Errorf("expected max mismatch line, got:\n%s", joined)
	}
	if !strings.Contains(joined, "KEDA idleReplicaCount=0 is set") {
		t.Errorf("expected idleReplicaCount observation line, got:\n%s", joined)
	}
}

// TestAnalyzeTriggers covers the trigger-matching heuristics: the no-triggers
// case, the matched-trigger case, and the unmatched-external-metric case.
func TestAnalyzeTriggers(t *testing.T) {
	t.Run("no triggers yields estimated warning", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		k := &Analysis{}
		lines := analyzeTriggers(hpa, k)
		if len(lines) != 1 {
			t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
		}
		if !strings.Contains(lines[0], "no triggers defined") {
			t.Fatalf("expected no-triggers warning, got: %q", lines[0])
		}
	})

	t.Run("matched trigger produces observed line with details", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{Name: "queue-depth"},
						},
					},
				},
			},
		}
		k := &Analysis{
			Triggers: []TriggerSummary{
				{Name: "queue", Type: "aws-sqs-queue", Threshold: "5", CurrentValue: "12"},
			},
		}
		lines := analyzeTriggers(hpa, k)
		joined := strings.Join(lines, "\n")
		// Matched line references the trigger, threshold, current value, and metric.
		if !strings.Contains(joined, `[observed]`) {
			t.Errorf("expected observed matched line, got:\n%s", joined)
		}
		if !strings.Contains(joined, `KEDA trigger "queue"`) || !strings.Contains(joined, "threshold=5") || !strings.Contains(joined, "current=12") {
			t.Errorf("expected trigger details in matched line, got:\n%s", joined)
		}
		// Trigger summary line still present.
		if !strings.Contains(joined, "ScaledObject defines 1 trigger(s): queue") {
			t.Errorf("expected trigger summary line, got:\n%s", joined)
		}
	})

	t.Run("unmatched external metric produces estimated line", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{Name: "mystery-metric"},
						},
					},
				},
			},
		}
		k := &Analysis{
			Triggers: []TriggerSummary{{Name: "queue", Type: "aws-sqs-queue"}},
		}
		lines := analyzeTriggers(hpa, k)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, `mystery-metric" has no matching KEDA trigger`) {
			t.Errorf("expected unmatched-metric estimated line, got:\n%s", joined)
		}
	})
}

// TestAnalyzePolling covers the polling-vs-stabilization comparison, which
// fires only when the polling interval is positive and exceeds the HPA
// scaleDown stabilization window.
func TestAnalyzePolling(t *testing.T) {
	t.Run("nil or non-positive interval returns nil", func(t *testing.T) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		if got := analyzePolling(hpa, &Analysis{}); got != nil {
			t.Fatalf("nil interval: expected nil, got %v", got)
		}
		zero := int32(0)
		if got := analyzePolling(hpa, &Analysis{PollingInterval: &zero}); got != nil {
			t.Fatalf("zero interval: expected nil, got %v", got)
		}
	})

	t.Run("stabilization window longer than interval produces warning", func(t *testing.T) {
		// analyzePolling warns when *window > interval: a long stabilization
		// window delays reaction to KEDA metric updates.
		window := int32(120)
		interval := int32(60)
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: &window,
					},
				},
			},
		}
		lines := analyzePolling(hpa, &Analysis{PollingInterval: &interval})
		if len(lines) != 1 {
			t.Fatalf("expected 1 polling warning line, got %d: %v", len(lines), lines)
		}
		if !strings.Contains(lines[0], "polling interval is 60s") || !strings.Contains(lines[0], "stabilization is 120s") {
			t.Errorf("unexpected polling warning: %q", lines[0])
		}
	})

	t.Run("window not exceeding interval produces no warning", func(t *testing.T) {
		window := int32(30)
		interval := int32(60)
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: &window,
					},
				},
			},
		}
		// window(30) not > interval(60) → no warning.
		if got := analyzePolling(hpa, &Analysis{PollingInterval: &interval}); len(got) != 0 {
			t.Fatalf("expected no polling warning, got %v", got)
		}
	})
}

// TestAnalyzeTriggerStatus covers the inactive-trigger detection branch that
// is otherwise hard to reach through the Analyze entrypoint.
func TestAnalyzeTriggerStatus(t *testing.T) {
	t.Run("nil analysis returns nil", func(t *testing.T) {
		if got := analyzeTriggerStatus(nil); got != nil {
			t.Fatalf("expected nil for nil analysis, got %v", got)
		}
	})

	t.Run("inactive trigger surfaces warning", func(t *testing.T) {
		k := &Analysis{
			Triggers: []TriggerSummary{
				{Name: "queue", Type: "aws-sqs-queue", Status: "Inactive"},
				{Name: "cpu", Type: "cpu", Status: "Active"},
			},
		}
		lines := analyzeTriggerStatus(k)
		if len(lines) != 1 {
			t.Fatalf("expected 1 inactive line, got %d: %v", len(lines), lines)
		}
		if !strings.Contains(lines[0], `trigger "queue"`) || !strings.Contains(lines[0], "Inactive") {
			t.Errorf("unexpected inactive warning: %q", lines[0])
		}
	})

	t.Run("all active triggers produce no warnings", func(t *testing.T) {
		k := &Analysis{
			Triggers: []TriggerSummary{{Name: "queue", Status: "Active"}},
		}
		if got := analyzeTriggerStatus(k); len(got) != 0 {
			t.Fatalf("expected no warnings for active triggers, got %v", got)
		}
	})
}
