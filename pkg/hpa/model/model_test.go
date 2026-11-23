package model

import "testing"

func TestIntWeight(t *testing.T) {
	got := IntWeight(0)
	if got == nil || *got != 0 {
		t.Fatalf("IntWeight(0) = %v, want a pointer to 0 (0 disables a penalty)", got)
	}
	if got == IntWeight(0) {
		t.Error("IntWeight(0) returned the same pointer twice; each call must allocate its own value")
	}
}

func TestHealthWeightsCloneIsIndependent(t *testing.T) {
	original := HealthWeights{
		ScalingInactive:     IntWeight(10),
		UnableToScale:       IntWeight(20),
		ScalingLimited:      IntWeight(30),
		ImplicitMaxReplicas: IntWeight(25),
		ScaleDownStabilized: IntWeight(15),
		AtMinimumReplicas:   IntWeight(5),
		KEDAInactiveTrigger: IntWeight(12),
		VPAConflict:         IntWeight(18),
		Churn:               IntWeight(8),
	}
	clone := original.Clone()

	fields := []struct {
		name             string
		original, cloned *int
	}{
		{"ScalingInactive", original.ScalingInactive, clone.ScalingInactive},
		{"UnableToScale", original.UnableToScale, clone.UnableToScale},
		{"ScalingLimited", original.ScalingLimited, clone.ScalingLimited},
		{"ImplicitMaxReplicas", original.ImplicitMaxReplicas, clone.ImplicitMaxReplicas},
		{"ScaleDownStabilized", original.ScaleDownStabilized, clone.ScaleDownStabilized},
		{"AtMinimumReplicas", original.AtMinimumReplicas, clone.AtMinimumReplicas},
		{"KEDAInactiveTrigger", original.KEDAInactiveTrigger, clone.KEDAInactiveTrigger},
		{"VPAConflict", original.VPAConflict, clone.VPAConflict},
		{"Churn", original.Churn, clone.Churn},
	}
	for _, field := range fields {
		if field.cloned == nil {
			t.Errorf("%s: clone lost the weight", field.name)
			continue
		}
		if field.cloned == field.original {
			t.Errorf("%s: clone shares the original pointer", field.name)
			continue
		}
		if *field.cloned != *field.original {
			t.Errorf("%s: clone = %d, want %d", field.name, *field.cloned, *field.original)
		}
		*field.cloned = 0
		if *field.original == 0 {
			t.Errorf("%s: zeroing the clone changed the original", field.name)
		}
	}

	// The loop zeroed the clone's Churn; a later change to the original must
	// not leak into it.
	*original.Churn = 99
	if clone.Churn == nil {
		t.Fatal("Churn in clone is nil after the original changed")
	}
	if *clone.Churn != 0 {
		t.Errorf("Churn in clone = %d, want the 0 written to the clone: the original's change leaked into it", *clone.Churn)
	}
}

func TestHealthWeightsCloneKeepsNil(t *testing.T) {
	if clone := (HealthWeights{}).Clone(); clone != (HealthWeights{}) {
		t.Errorf("Clone() of all-nil weights = %+v, want all nil (nil selects the default)", clone)
	}
}
