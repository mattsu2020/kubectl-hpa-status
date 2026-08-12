package hpa

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/churn"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

func TestApplyEnrichmentPenalties(t *testing.T) {
	// inactiveKEDA returns a KEDAAnalysis whose only trigger is Inactive, which
	// triggers the KEDA penalty.
	inactiveKEDA := func() *KEDAAnalysis {
		return &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Type: "prometheus", Status: "Inactive"}},
		}
	}
	// activeKEDA returns a KEDAAnalysis whose trigger is Active (no penalty).
	activeKEDA := func() *KEDAAnalysis {
		return &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Type: "prometheus", Status: "Active"}},
		}
	}
	vpaConflict := func() *vpa.ConflictInfo {
		return &vpa.ConflictInfo{VPAName: "my-vpa", UpdateMode: "Auto"}
	}

	tests := []struct {
		name       string
		health     string
		score      int
		keda       func() *KEDAAnalysis
		vpa        func() *vpa.ConflictInfo
		weights    HealthWeights
		wantScore  int
		wantHealth string
	}{
		{name: "KEDAInactiveTrigger", health: "OK", score: 95, keda: inactiveKEDA, wantScore: 80, wantHealth: "LIMITED"},
		{name: "VPAConflict", health: "OK", score: 95, vpa: vpaConflict, wantScore: 75, wantHealth: "LIMITED"},
		{name: "BothPenalties", health: "OK", score: 95, keda: inactiveKEDA, vpa: vpaConflict, wantScore: 60, wantHealth: "LIMITED"},
		{name: "NilEnrichment", health: "OK", score: 95, wantScore: 95, wantHealth: "OK"},
		{name: "CustomWeights", health: "OK", score: 95, keda: inactiveKEDA, vpa: vpaConflict,
			weights:   HealthWeights{KEDAInactiveTrigger: IntWeight(30), VPAConflict: IntWeight(40)},
			wantScore: 25, wantHealth: "LIMITED"},
		{name: "ScoreNotBelowZero", health: "OK", score: 10, keda: inactiveKEDA, vpa: vpaConflict, wantScore: 0, wantHealth: "LIMITED"},
		{name: "DoesNotDowngradeERROR", health: "ERROR", score: 55, keda: inactiveKEDA, wantScore: 40, wantHealth: "ERROR"},
		{name: "KEDAHealthyTriggersNoPenalty", health: "OK", score: 95, keda: activeKEDA, wantScore: 95, wantHealth: "OK"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &Analysis{Health: tc.health, HealthScore: tc.score}
			if tc.keda != nil {
				a.KEDAInfo = tc.keda()
			}
			if tc.vpa != nil {
				a.VPAConflict = tc.vpa()
			}
			ApplyEnrichmentPenalties(a, tc.weights)
			if a.HealthScore != tc.wantScore {
				t.Errorf("HealthScore = %d, want %d", a.HealthScore, tc.wantScore)
			}
			if a.Health != tc.wantHealth {
				t.Errorf("Health = %q, want %q", a.Health, tc.wantHealth)
			}
		})
	}
}

// TestApplyEnrichmentPenalties_NilAnalysis verifies the function is nil-safe.

func TestApplyEnrichmentPenalties_NilAnalysis(_ *testing.T) {
	ApplyEnrichmentPenalties(nil, HealthWeights{})
	// Should not panic.
}

func TestApplyEnrichmentPenaltiesIsIdempotentAcrossFinalize(t *testing.T) {
	remaining := int64(30)
	a := Analysis{
		Health:                 string(HealthOK),
		HealthScore:            95,
		StabilizationRemaining: &remaining,
		KEDAInfo: &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Type: "prometheus", Status: "Inactive"}},
		},
		VPAConflict: &vpa.ConflictInfo{VPAName: "my-vpa", UpdateMode: "Auto"},
	}

	ApplyEnrichmentPenalties(&a, HealthWeights{})
	firstScore := a.HealthScore
	firstSignalCount := len(a.HealthResult.Signals)

	ApplyEnrichmentPenalties(&a, HealthWeights{})
	if a.HealthScore != firstScore {
		t.Fatalf("second application changed HealthScore from %d to %d", firstScore, a.HealthScore)
	}
	if len(a.HealthResult.Signals) != firstSignalCount {
		t.Fatalf("second application duplicated signals: first=%d second=%d", firstSignalCount, len(a.HealthResult.Signals))
	}

	finalized := FinalizeAnalysis(a)
	ApplyEnrichmentPenalties(&finalized, HealthWeights{})
	if finalized.HealthScore != firstScore {
		t.Fatalf("application after FinalizeAnalysis changed HealthScore from %d to %d", firstScore, finalized.HealthScore)
	}
	if finalized.HealthResult == nil {
		t.Fatal("HealthResult must be initialized by enrichment")
	}
	if finalized.HealthResult.Score != finalized.HealthScore ||
		string(finalized.HealthResult.State) != finalized.Health {
		t.Fatalf("flat health fields and HealthResult diverged: health=%q score=%d result=%+v",
			finalized.Health, finalized.HealthScore, finalized.HealthResult)
	}
	if len(finalized.HealthResult.Signals) != firstSignalCount {
		t.Fatalf("application after FinalizeAnalysis duplicated signals: first=%d final=%d",
			firstSignalCount, len(finalized.HealthResult.Signals))
	}
}

func TestApplyChurnPenaltyIsIdempotentAndInitializesTypedResult(t *testing.T) {
	a := Analysis{
		Health:        string(HealthOK),
		HealthScore:   90,
		ChurnAnalysis: &churn.ChurnAnalysis{Level: churn.ChurnHigh},
	}

	ApplyChurnPenalty(&a, HealthWeights{})
	firstScore := a.HealthScore
	ApplyChurnPenalty(&a, HealthWeights{})

	if a.HealthScore != firstScore {
		t.Fatalf("second churn penalty changed score from %d to %d", firstScore, a.HealthScore)
	}
	if a.HealthResult == nil {
		t.Fatal("churn penalty did not initialize HealthResult")
	}
	if a.HealthResult.Score != a.HealthScore || string(a.HealthResult.State) != a.Health {
		t.Fatalf("flat health fields and HealthResult diverged: health=%q score=%d result=%+v",
			a.Health, a.HealthScore, a.HealthResult)
	}
	if len(a.HealthResult.Signals) != 1 || a.HealthResult.Signals[0].Reason != enrichmentPenaltyChurn {
		t.Fatalf("churn signals = %+v, want one stable signal", a.HealthResult.Signals)
	}
}

func TestDynamicHealthPenaltiesRecomputeFromUnclampedBaseline(t *testing.T) {
	a := Analysis{
		Health:      string(HealthOK),
		HealthScore: 10,
		KEDAInfo: &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Status: "Inactive"}},
		},
	}
	ApplyEnrichmentPenalties(&a, HealthWeights{KEDAInactiveTrigger: IntWeight(15)})
	if a.HealthScore != 0 {
		t.Fatalf("first score = %d, want 0", a.HealthScore)
	}
	ApplyEnrichmentPenalties(&a, HealthWeights{KEDAInactiveTrigger: IntWeight(5)})
	if a.HealthScore != 5 {
		t.Fatalf("reweighted score = %d, want 5", a.HealthScore)
	}
}

func TestDynamicHealthPenaltiesRemoveInactiveSignals(t *testing.T) {
	a := Analysis{
		Health:      string(HealthOK),
		HealthScore: 90,
		KEDAInfo: &KEDAAnalysis{
			Triggers: []KEDATriggerSummary{{Status: "Inactive"}},
		},
		VPAConflict: &vpa.ConflictInfo{VPAName: "web-vpa"},
		ChurnAnalysis: &churn.ChurnAnalysis{
			Level: churn.ChurnHigh,
		},
	}
	ApplyChurnPenalty(&a, HealthWeights{})
	if a.Health != string(HealthLimited) || len(a.HealthResult.Signals) != 3 {
		t.Fatalf("dynamic health = %+v", a.HealthResult)
	}

	a.KEDAInfo.Triggers[0].Status = "Active"
	a.VPAConflict = nil
	a.ChurnAnalysis.Level = churn.ChurnLow
	ApplyEnrichmentPenalties(&a, HealthWeights{})
	if a.HealthScore != 90 || a.Health != string(HealthOK) {
		t.Fatalf("restored health = %s/%d, want OK/90", a.Health, a.HealthScore)
	}
	if len(a.HealthResult.Signals) != 0 {
		t.Fatalf("stale dynamic signals remain: %+v", a.HealthResult.Signals)
	}
}

func TestDynamicHealthPenaltiesRestoreBaseStateAfterJSONRoundTrip(t *testing.T) {
	original := Analysis{
		Health:      string(HealthOK),
		HealthScore: 100,
		VPAConflict: &vpa.ConflictInfo{VPAName: "web-vpa"},
	}
	ApplyEnrichmentPenalties(&original, HealthWeights{})

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Analysis
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	restored.VPAConflict = nil
	ApplyEnrichmentPenalties(&restored, HealthWeights{})

	if restored.Health != string(HealthOK) || restored.HealthScore != 100 {
		t.Fatalf("round-trip health = %s/%d, want OK/100", restored.Health, restored.HealthScore)
	}
	if restored.HealthResult == nil || restored.HealthResult.State != HealthOK || len(restored.HealthResult.Signals) != 0 {
		t.Fatalf("round-trip HealthResult = %+v", restored.HealthResult)
	}
}

func TestHealthScorePenaltyGating(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(hpa *autoscalingv2.HorizontalPodAutoscaler)
		wantFullScore bool // true: expect score==100; false: expect score<100
	}{
		{
			name: "NoMinReplicasPenaltyWithoutScalingLimited",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				// At minReplicas=2 but no ScalingLimited — normal low-traffic state.
				hpa.Status.CurrentReplicas = 2
				hpa.Status.DesiredReplicas = 2
			},
			wantFullScore: true,
		},
		{
			name: "MinReplicasPenaltyWithScalingLimited",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 2
				hpa.Status.DesiredReplicas = 2
				hpa.Status.Conditions = append(hpa.Status.Conditions,
					autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooFewReplicas"},
				)
			},
			wantFullScore: false,
		},
		{
			name: "ImplicitMaxReplicas_NoPenaltyWithoutPressure",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				// No ScalingLimited, no metric above target — no penalty expected.
			},
			wantFullScore: true,
		},
		{
			name: "ImplicitMaxReplicas_PenaltyWithMetricPressure",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 80)}
				hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 90)} // ratio > 1.0
			},
			wantFullScore: false,
		},
		{
			name: "ImplicitMaxReplicas_PenaltyWhileCurrentReplicasConverge",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 9
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 80)}
				hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 90)}
			},
			wantFullScore: false,
		},
		{
			name: "ImplicitMaxReplicas_PenaltyWithScalingLimited",
			setup: func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Status.CurrentReplicas = 10
				hpa.Status.DesiredReplicas = 10
				hpa.Spec.MaxReplicas = 10
				hpa.Status.Conditions = append(hpa.Status.Conditions,
					autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
				)
			},
			wantFullScore: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hpa := baseHPA()
			tc.setup(hpa)
			_, score := Health(hpa, 2)
			if tc.wantFullScore && score != 100 {
				t.Fatalf("score = %d, want 100 (no penalty)", score)
			}
			if !tc.wantFullScore && score >= 100 {
				t.Fatalf("score = %d, want < 100 (penalty expected)", score)
			}
		})
	}
}

func TestHealthWeightsExplicitZeroDisablesPenalty(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.Conditions = append(hpa.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{Type: "ScalingLimited", Status: corev1.ConditionTrue, Reason: "TooManyReplicas"},
	)
	_, defaultScore := Health(hpa, 2)

	// With ScalingLimited explicitly set to 0, penalty should be disabled.
	zeroResult := HealthWithWeights(hpa, 2, HealthWeights{ScalingLimited: IntWeight(0)})
	zeroScore := zeroResult.Score
	if zeroScore != defaultScore+healthPenaltyScalingLimited {
		t.Fatalf("expected %d (score without ScalingLimited penalty), got %d", defaultScore+healthPenaltyScalingLimited, zeroScore)
	}
}

func TestHealthDoesNotDoubleCountExplicitAndImplicitMaxLimit(t *testing.T) {
	hpa := baseHPA()
	hpa.Status.CurrentReplicas = 10
	hpa.Status.DesiredReplicas = 10
	hpa.Spec.MaxReplicas = 10
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{resourceMetricSpec(corev1.ResourceCPU, 80)}
	hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{resourceMetricStatus(corev1.ResourceCPU, 100)}
	hpa.Status.Conditions = append(hpa.Status.Conditions,
		autoscalingv2.HorizontalPodAutoscalerCondition{Type: ConditionScalingLimited, Status: corev1.ConditionTrue, Reason: "TooManyReplicas"})

	result := HealthWithWeights(hpa, 2, HealthWeights{})
	if result.Score != healthScoreMax-healthPenaltyScalingLimited {
		t.Fatalf("score = %d, want only ScalingLimited penalty (%d)", result.Score, healthScoreMax-healthPenaltyScalingLimited)
	}
	for _, signal := range result.Signals {
		if strings.Contains(signal.Reason, "Implicit maxReplicas") {
			t.Fatalf("implicit ceiling signal must not duplicate ScalingLimited: %+v", result.Signals)
		}
	}
}
