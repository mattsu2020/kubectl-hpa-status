package hpa

import (
	"testing"
)

func TestNewHealthAccumulator(t *testing.T) {
	acc := NewHealthAccumulator(100)
	if acc.Result().Score != 100 {
		t.Errorf("expected initial score 100, got %d", acc.Result().Score)
	}
	if len(acc.Result().Signals) != 0 {
		t.Errorf("expected no initial signals, got %d", len(acc.Result().Signals))
	}
	if acc.Result().State != "" {
		t.Errorf("expected empty initial state, got %s", acc.Result().State)
	}
}

func TestHealthAccumulator_AddPenalty(t *testing.T) {
	acc := NewHealthAccumulator(100)
	acc.AddPenalty("ScalingActive is not True", 45, HealthError)

	result := acc.Result()
	if result.Score != 55 {
		t.Errorf("expected score 55 (100-45), got %d", result.Score)
	}
	if len(result.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(result.Signals))
	}
	if result.Signals[0].Reason != "ScalingActive is not True" {
		t.Errorf("unexpected reason: %s", result.Signals[0].Reason)
	}
	if result.Signals[0].Penalty != 45 {
		t.Errorf("unexpected penalty: %d", result.Signals[0].Penalty)
	}
	if result.Signals[0].Severity != HealthError {
		t.Errorf("unexpected severity: %s", result.Signals[0].Severity)
	}
}

func TestHealthAccumulator_MultiplePenalties(t *testing.T) {
	acc := NewHealthAccumulator(100)
	acc.AddPenalty("ScalingLimited", 25, HealthLimited)
	acc.AddPenalty("ScaleDownStabilized", 10, HealthStabilized)

	result := acc.Result()
	if result.Score != 65 {
		t.Errorf("expected score 65 (100-25-10), got %d", result.Score)
	}
	if len(result.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(result.Signals))
	}
}

func TestHealthAccumulator_SetState(t *testing.T) {
	acc := NewHealthAccumulator(100)
	acc.SetState(HealthError)

	result := acc.Result()
	if result.State != HealthError {
		t.Errorf("expected state ERROR, got %s", result.State)
	}
}

func TestHealthAccumulator_ResultImmutable(t *testing.T) {
	acc := NewHealthAccumulator(100)
	acc.AddPenalty("test", 10, HealthLimited)

	result1 := acc.Result()
	// Modify the returned result
	result1.Score = 999
	result1.Signals[0] = HealthSignal{Reason: "modified"}

	// Original accumulator should be unchanged
	result2 := acc.Result()
	if result2.Score != 90 {
		t.Errorf("expected score 90 unchanged, got %d", result2.Score)
	}
	if len(result2.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(result2.Signals))
	}
	if result2.Signals[0].Reason != "test" {
		t.Errorf("expected original reason 'test', got %s", result2.Signals[0].Reason)
	}
}
