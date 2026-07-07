// Package replaylab holds the report model and renderers for the replay-lab
// command (what-if policy simulation over recorded traces). It follows the
// cmd/bundle precedent: presentation and its types live here, while the
// trace loading and simulation orchestration stay in cmd/replay_lab.go.
package replaylab

// Impact holds computed percentage changes between current and proposed.
type Impact struct {
	ScaleEventReductionPct float64 `json:"scaleEventReductionPct,omitempty" yaml:"scaleEventReductionPct,omitempty"`
	PodHoursChangePct      float64 `json:"podHoursChangePct,omitempty" yaml:"podHoursChangePct,omitempty"`
	UnderProvisionFixed    bool    `json:"underProvisionFixed,omitempty" yaml:"underProvisionFixed,omitempty"`
	AdditionalWorstCase    int32   `json:"additionalWorstCase,omitempty" yaml:"additionalWorstCase,omitempty"`
	NoMissedScaleUp        bool    `json:"noMissedScaleUp,omitempty" yaml:"noMissedScaleUp,omitempty"`
}

// Report is the full replay-lab result: the current trace summary plus the
// simulated candidate outcomes and derived recommendations.
type Report struct {
	Namespace       string            `json:"namespace" yaml:"namespace"`
	Name            string            `json:"name" yaml:"name"`
	Record          string            `json:"record" yaml:"record"`
	Score           []string          `json:"score,omitempty" yaml:"score,omitempty"`
	Candidate       string            `json:"candidate,omitempty" yaml:"candidate,omitempty"`
	ProposedConfig  map[string]string `json:"proposedConfig,omitempty" yaml:"proposedConfig,omitempty"`
	Current         Summary           `json:"current" yaml:"current"`
	CandidateResult *Summary          `json:"candidateResult,omitempty" yaml:"candidateResult,omitempty"`
	Candidates      []CandidateResult `json:"candidates,omitempty" yaml:"candidates,omitempty"`
	Impact          *Impact           `json:"impact,omitempty" yaml:"impact,omitempty"`
	Recommendation  string            `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
	Recommendations []string          `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
	Limitations     []string          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

// CandidateResult is one simulated candidate configuration and its summary.
type CandidateResult struct {
	Name           string            `json:"name" yaml:"name"`
	Candidate      string            `json:"candidate" yaml:"candidate"`
	ProposedConfig map[string]string `json:"proposedConfig,omitempty" yaml:"proposedConfig,omitempty"`
	Summary        Summary           `json:"summary" yaml:"summary"`
	Recommendation string            `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
}

// Summary aggregates scaling behavior over one replayed trace: event counts,
// replica peaks, capped time at maxReplicas, and the flapping score.
type Summary struct {
	Snapshots               int     `json:"snapshots" yaml:"snapshots"`
	ScaleEvents             int     `json:"scaleEvents" yaml:"scaleEvents"`
	DirectionFlips          int     `json:"directionFlips" yaml:"directionFlips"`
	PeakReplicas            int32   `json:"peakReplicas" yaml:"peakReplicas"`
	MaxReplicas             int32   `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	MaxReplicasReached      int     `json:"maxReplicasReached" yaml:"maxReplicasReached"`
	CappedDurationSeconds   int64   `json:"cappedDurationSeconds" yaml:"cappedDurationSeconds"`
	CappedDuration          string  `json:"cappedDuration" yaml:"cappedDuration"`
	EstimatedUnderProvision int     `json:"estimatedUnderProvisionWindows" yaml:"estimatedUnderProvisionWindows"`
	PodHours                float64 `json:"podHours" yaml:"podHours"`
	ExtraPodHours           float64 `json:"extraPodHours,omitempty" yaml:"extraPodHours,omitempty"`
	AdditionalWorstCasePods int32   `json:"additionalWorstCasePods,omitempty" yaml:"additionalWorstCasePods,omitempty"`
	FlappingScore           string  `json:"flappingScore" yaml:"flappingScore"`
	FlappingLabel           string  `json:"flappingLabel,omitempty" yaml:"flappingLabel,omitempty"`
}
