package tui

import (
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// renderDetail* helpers are pure functions over *strings.Builder and an
// Analysis/ListItem/StatusReport. This file exercises each one against a
// representative input and asserts the output contains the section's
// identifying heading and key field values. Empty-input cases verify the
// early-return guard so absent data does not emit stray sections.

// --- helpers ---

func renderToString(fn func(*strings.Builder)) string {
	var sb strings.Builder
	fn(&sb)
	return sb.String()
}

func testListItem() hpaanalysis.ListItem {
	return hpaanalysis.ListItem{
		Namespace:   "production",
		Name:        "web",
		Target:      "Deployment/web",
		Health:      "OK",
		HealthScore: 95,
		Current:     5,
		Desired:     7,
		Min:         2,
		Max:         20,
		Summary:     "HPA currently wants to scale up.",
	}
}

func int64Ptr(v int64) *int64 { return &v }

// --- ListItem renderers ---

func TestRenderDetailHeader(t *testing.T) {
	out := renderToString(func(sb *strings.Builder) { renderDetailHeader(sb, testListItem()) })
	for _, want := range []string{"HPA production/web", "Target: Deployment/web"} {
		if !strings.Contains(out, want) {
			t.Errorf("header: want %q in output, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailHealth(t *testing.T) {
	out := renderToString(func(sb *strings.Builder) { renderDetailHealth(sb, testListItem()) })
	// Score value and the label always appear; the bar is style-wrapped.
	for _, want := range []string{"Health:", "95/100", "OK"} {
		if !strings.Contains(out, want) {
			t.Errorf("health: want %q in output, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailReplicas(t *testing.T) {
	out := renderToString(func(sb *strings.Builder) { renderDetailReplicas(sb, testListItem()) })
	for _, want := range []string{
		"current=5",
		"desired=7",
		"diff=+2", // desired-current formatted with sign
		"min=2",
		"max=20",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("replicas: want %q in output, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailReplicas_NegativeDiff(t *testing.T) {
	item := testListItem()
	item.Current, item.Desired = 10, 3 // scale-down diff
	out := renderToString(func(sb *strings.Builder) { renderDetailReplicas(sb, item) })
	if !strings.Contains(out, "diff=-7") {
		t.Errorf("replicas: want diff=-7 for scale-down, got:\n%s", out)
	}
}

func TestRenderDetailSummary(t *testing.T) {
	out := renderToString(func(sb *strings.Builder) { renderDetailSummary(sb, testListItem()) })
	if !strings.Contains(out, "Summary: HPA currently wants to scale up.") {
		t.Errorf("summary: missing Summary line, got:\n%s", out)
	}
}

// --- Score breakdown ---

func TestRenderDetailScoreBreakdown_WithSignals(t *testing.T) {
	report := &hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			HealthScore: 55,
			HealthResult: &hpaanalysis.HealthResult{
				Signals: []hpaanalysis.HealthSignal{
					{Penalty: 25, Reason: "ScalingLimited", Severity: "LIMITED"},
					{Penalty: 20, Reason: "implicit maxReplicas", Severity: "LIMITED"},
				},
			},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailScoreBreakdown(sb, report) })
	for _, want := range []string{"Score Breakdown:", "base: 100", "-25 ScalingLimited (LIMITED)", "final: 55"} {
		if !strings.Contains(out, want) {
			t.Errorf("score breakdown: want %q in output, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailScoreBreakdown_NoSignals(t *testing.T) {
	// No HealthResult -> empty output (early return).
	report := &hpaanalysis.StatusReport{Analysis: hpaanalysis.Analysis{}}
	out := renderToString(func(sb *strings.Builder) { renderDetailScoreBreakdown(sb, report) })
	if out != "" {
		t.Errorf("score breakdown: expected empty output when no signals, got %q", out)
	}
}

// --- Hidden factors ---

func TestRenderDetailHiddenFactors(t *testing.T) {
	a := &hpaanalysis.Analysis{
		HiddenFactors: []hpaanalysis.HiddenDecisionFactor{
			{Name: "tolerance", Status: "assumed 0.1", Confidence: "medium", Impact: "small adjustments may not trigger scaling"},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailHiddenFactors(sb, a) })
	for _, want := range []string{"Hidden decision factors:", "tolerance: assumed 0.1 (medium)", "small adjustments may not trigger scaling"} {
		if !strings.Contains(out, want) {
			t.Errorf("hidden factors: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailHiddenFactors_Empty(t *testing.T) {
	a := &hpaanalysis.Analysis{}
	out := renderToString(func(sb *strings.Builder) { renderDetailHiddenFactors(sb, a) })
	if out != "" {
		t.Errorf("hidden factors: expected empty output when none, got %q", out)
	}
}

// --- Stabilization ---

func TestRenderDetailStabilization_Active(t *testing.T) {
	a := &hpaanalysis.Analysis{
		StabilizationRemaining:     int64Ptr(45),
		StabilizationWindowSeconds: int32Ptr(300),
		StabilizationSource:        "scaleDown",
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailStabilization(sb, a) })
	for _, want := range []string{"Stabilized (scaleDown)", "[estimated]"} {
		if !strings.Contains(out, want) {
			t.Errorf("stabilization: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailStabilization_Inactive(t *testing.T) {
	a := &hpaanalysis.Analysis{} // no remaining
	out := renderToString(func(sb *strings.Builder) { renderDetailStabilization(sb, a) })
	if out != "" {
		t.Errorf("stabilization: expected empty when inactive, got %q", out)
	}
}

// --- Conditions ---

func TestRenderDetailConditions(t *testing.T) {
	a := &hpaanalysis.Analysis{
		Conditions: []hpaanalysis.Condition{
			{Type: "ScalingActive", Status: "True", Reason: "ValidMetricFound"},
			{Type: "AbleToScale", Status: "False", Reason: "Backoff"},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailConditions(sb, a) })
	for _, want := range []string{"Conditions:", "ScalingActive", "AbleToScale", "ValidMetricFound"} {
		if !strings.Contains(out, want) {
			t.Errorf("conditions: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailConditions_Empty(t *testing.T) {
	a := &hpaanalysis.Analysis{}
	out := renderToString(func(sb *strings.Builder) { renderDetailConditions(sb, a) })
	if out != "" {
		t.Errorf("conditions: expected empty when none, got %q", out)
	}
}

// --- Metrics ---

func TestRenderDetailMetrics(t *testing.T) {
	ratio := 1.5
	a := &hpaanalysis.Analysis{
		Metrics: []hpaanalysis.Metric{
			{Name: "cpu", Current: "92%", Target: "60%", Ratio: &ratio},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailMetrics(sb, a) })
	for _, want := range []string{"Metrics:", "cpu", "current=92%", "target=60%", "ratio=1.500"} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailMetrics_FallsBackToType(t *testing.T) {
	// When Name is empty, the Type label is shown.
	a := &hpaanalysis.Analysis{
		Metrics: []hpaanalysis.Metric{{Type: "Resource", Current: "10%", Target: "80%", Text: "Resource cpu current=10% target=80%"}},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailMetrics(sb, a) })
	if !strings.Contains(out, "Resource") {
		t.Errorf("metrics: expected Type fallback 'Resource', got:\n%s", out)
	}
}

// --- Actions / Interpretation / Decision signals ---

func TestRenderDetailActions(t *testing.T) {
	a := &hpaanalysis.Analysis{Actions: []string{"Raise maxReplicas", "Check node capacity"}}
	out := renderToString(func(sb *strings.Builder) { renderDetailActions(sb, a) })
	if !strings.Contains(out, "Actions:") || !strings.Contains(out, "Raise maxReplicas") {
		t.Errorf("actions: missing heading or value, got:\n%s", out)
	}
}

func TestRenderDetailInterpretation_Truncates(t *testing.T) {
	// More than maxLines (5) -> "and N more" line.
	lines := make([]string, 8)
	for i := range lines {
		lines[i] = "line"
	}
	a := &hpaanalysis.Analysis{Interpretation: lines}
	out := renderToString(func(sb *strings.Builder) { renderDetailInterpretation(sb, a) })
	if !strings.Contains(out, "and 3 more") {
		t.Errorf("interpretation: expected truncation notice 'and 3 more', got:\n%s", out)
	}
}

func TestRenderDetailDecisionSignals(t *testing.T) {
	a := &hpaanalysis.Analysis{
		DecisionSignals: []hpaanalysis.DecisionSignal{
			{Reason: "DesiredWithinTolerance", Message: "all metrics within target", MetricName: "cpu", Confidence: "high"},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailDecisionSignals(sb, a) })
	for _, want := range []string{"Decision Signals:", "DesiredWithinTolerance", "metric=cpu", "[high]"} {
		if !strings.Contains(out, want) {
			t.Errorf("decision signals: want %q, got:\n%s", want, out)
		}
	}
}

// --- Target replicas ---

func TestRenderDetailTargetReplicas_NotReady(t *testing.T) {
	a := &hpaanalysis.Analysis{
		TargetReplicas: &hpaanalysis.TargetReplicaInfo{
			TotalReplicas: 10,
			NotReady:      3,
			Pending:       1,
			Unschedulable: 1,
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailTargetReplicas(sb, a) })
	for _, want := range []string{"3 of 10 pods not ready", "1 pods pending (1 unschedulable)"} {
		if !strings.Contains(out, want) {
			t.Errorf("target replicas: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailTargetReplicas_Nil(t *testing.T) {
	a := &hpaanalysis.Analysis{}
	out := renderToString(func(sb *strings.Builder) { renderDetailTargetReplicas(sb, a) })
	if out != "" {
		t.Errorf("target replicas: expected empty when nil, got %q", out)
	}
}

// --- KEDA ---

func TestRenderDetailKEDA(t *testing.T) {
	polling := int32(30)
	cooldown := int32(300)
	a := &hpaanalysis.Analysis{
		KEDAInfo: &hpaanalysis.KEDAAnalysis{
			ScaledObjectName: "web-scaledobject",
			Triggers: []hpaanalysis.KEDATriggerSummary{
				{Type: "kafka", Name: "keda-kafka", Status: "Active", MetricName: "lag", Threshold: "5", CurrentValue: "12"},
			},
			PollingInterval: &polling,
			CooldownPeriod:  &cooldown,
			Fallback:        &hpaanalysis.KEDAFallbackInfo{FailureThreshold: 3, Replicas: 1},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailKEDA(sb, a) })
	for _, want := range []string{
		"KEDA:",
		"ScaledObject: web-scaledobject",
		"Triggers:",
		"kafka (keda-kafka)", // type + name
		"metric=lag", "threshold=5", "current=12",
		"Polling interval: 30s",
		"Cooldown period: 300s",
		"Fallback: failureThreshold=3, replicas=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("keda: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailKEDA_Nil(t *testing.T) {
	a := &hpaanalysis.Analysis{}
	out := renderToString(func(sb *strings.Builder) { renderDetailKEDA(sb, a) })
	if out != "" {
		t.Errorf("keda: expected empty when nil, got %q", out)
	}
}

func TestRenderDetailKEDATrigger_AuthRef(t *testing.T) {
	out := renderToString(func(sb *strings.Builder) {
		renderDetailKEDATrigger(sb, hpaanalysis.KEDATriggerSummary{Type: "prometheus", AuthRef: "keda-trigger-auth"})
	})
	if !strings.Contains(out, "authRef=keda-trigger-auth") {
		t.Errorf("keda trigger: expected authRef line, got:\n%s", out)
	}
}

// --- Suggestions / VPA ---

func TestRenderDetailSuggestions(t *testing.T) {
	a := &hpaanalysis.Analysis{
		Suggestions: []hpaanalysis.Suggestion{
			{Title: "Raise maxReplicas to 30", Risk: "medium"},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailSuggestions(sb, a) })
	for _, want := range []string{"Suggestions:", "Raise maxReplicas to 30 (medium)", "--fix --apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("suggestions: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailVPA(t *testing.T) {
	a := &hpaanalysis.Analysis{
		VPAConflict: &hpaanalysis.VPAConflictInfo{
			VPAName:    "web-vpa",
			UpdateMode: "Auto",
			Recommendations: []hpaanalysis.VPARecommendation{
				{Container: "app", Resource: "cpu", Target: "500m"},
			},
		},
	}
	out := renderToString(func(sb *strings.Builder) { renderDetailVPA(sb, a) })
	for _, want := range []string{"VPA:", "web-vpa updateMode=Auto", "app/cpu target=500m"} {
		if !strings.Contains(out, want) {
			t.Errorf("vpa: want %q, got:\n%s", want, out)
		}
	}
}

func TestRenderDetailVPA_Nil(t *testing.T) {
	a := &hpaanalysis.Analysis{}
	out := renderToString(func(sb *strings.Builder) { renderDetailVPA(sb, a) })
	if out != "" {
		t.Errorf("vpa: expected empty when nil, got %q", out)
	}
}

// --- orchestrator smoke test ---

func TestRenderDetailView_NoSelection(t *testing.T) {
	// No items selected -> early return with placeholder.
	m := NewModel(nil, "default", Options{})
	out := m.renderDetailView()
	if !strings.Contains(out, "No HPA selected.") {
		t.Errorf("renderDetailView: expected 'No HPA selected.' when cursor out of range, got:\n%s", out)
	}
}
