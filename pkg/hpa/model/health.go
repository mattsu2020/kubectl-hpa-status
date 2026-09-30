package model

// HealthWeights holds health penalties. nil selects the default; 0 disables a penalty.
type HealthWeights struct {
	ScalingInactive     *int `json:"scalingInactive,omitempty" yaml:"scalingInactive,omitempty"`
	UnableToScale       *int `json:"unableToScale,omitempty" yaml:"unableToScale,omitempty"`
	ScalingLimited      *int `json:"scalingLimited,omitempty" yaml:"scalingLimited,omitempty"`
	ImplicitMaxReplicas *int `json:"implicitMaxReplicas,omitempty" yaml:"implicitMaxReplicas,omitempty"`
	ScaleDownStabilized *int `json:"scaleDownStabilized,omitempty" yaml:"scaleDownStabilized,omitempty"`
	AtMinimumReplicas   *int `json:"atMinimumReplicas,omitempty" yaml:"atMinimumReplicas,omitempty"`
	KEDAInactiveTrigger *int `json:"kedaInactiveTrigger,omitempty" yaml:"kedaInactiveTrigger,omitempty"`
	VPAConflict         *int `json:"vpaConflict,omitempty" yaml:"vpaConflict,omitempty"`
	Churn               *int `json:"churn,omitempty" yaml:"churn,omitempty"`
}

// IntWeight returns a pointer to the given int value. Use this to set
// explicit HealthWeights values, including 0 to disable a penalty.
func IntWeight(v int) *int { return &v }

// Clone returns a deep copy of the weights. Each *int field is independently
// allocated so mutating one copy (e.g. flipping a weight to zero to disable a
// penalty) does not leak into the other. nil pointers stay nil. Use this when
// a Root copy needs to diverge its health-weight configuration.
func (w HealthWeights) Clone() HealthWeights {
	clonePtr := func(p *int) *int {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	return HealthWeights{
		ScalingInactive:     clonePtr(w.ScalingInactive),
		UnableToScale:       clonePtr(w.UnableToScale),
		ScalingLimited:      clonePtr(w.ScalingLimited),
		ImplicitMaxReplicas: clonePtr(w.ImplicitMaxReplicas),
		ScaleDownStabilized: clonePtr(w.ScaleDownStabilized),
		AtMinimumReplicas:   clonePtr(w.AtMinimumReplicas),
		KEDAInactiveTrigger: clonePtr(w.KEDAInactiveTrigger),
		VPAConflict:         clonePtr(w.VPAConflict),
		Churn:               clonePtr(w.Churn),
	}
}
