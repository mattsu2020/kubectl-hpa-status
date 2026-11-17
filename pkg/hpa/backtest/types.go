// Package backtest replays recorded or imported HPA timeline traces against
// candidate HPA configurations to validate tuning and pre-scale proposals
// before applying them to a cluster.
package backtest

import (
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const (
	// DefaultTolerance is the Kubernetes HPA controller's default 10% deadband.
	DefaultTolerance = 0.10

	// Confidence levels indicate how much of the trace carried typed metric
	// readings versus falling back to recorded desiredReplicas.
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// PrescaleWindow raises the effective minReplicas during a recurring time-of-day
// window (e.g. a seasonality pre-scale cron schedule).
type PrescaleWindow struct {
	StartMinute int            `json:"startMinute" yaml:"startMinute"` // 0..1439 inclusive
	EndMinute   int            `json:"endMinute" yaml:"endMinute"`     // 1..1440 inclusive; if EndMinute <= StartMinute, wraps past midnight
	MinReplicas int32          `json:"minReplicas" yaml:"minReplicas"`
	Weekdays    []time.Weekday `json:"weekdays,omitempty" yaml:"weekdays,omitempty"`
}

// CandidateSpec defines one HPA configuration variant to backtest.
type CandidateSpec struct {
	Name                          string                         `json:"name" yaml:"name"`
	MinReplicas                   *int32                         `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	MaxReplicas                   int32                          `json:"maxReplicas" yaml:"maxReplicas"`
	TargetUtilization             *int32                         `json:"targetUtilization,omitempty" yaml:"targetUtilization,omitempty"`
	TargetValue                   *float64                       `json:"targetValue,omitempty" yaml:"targetValue,omitempty"`
	Tolerance                     float64                        `json:"tolerance,omitempty" yaml:"tolerance,omitempty"`
	ScaleUpStabilizationSeconds   int32                          `json:"scaleUpStabilizationSeconds,omitempty" yaml:"scaleUpStabilizationSeconds,omitempty"`
	ScaleDownStabilizationSeconds int32                          `json:"scaleDownStabilizationSeconds,omitempty" yaml:"scaleDownStabilizationSeconds,omitempty"`
	ScaleUpRules                  *autoscalingv2.HPAScalingRules `json:"scaleUpRules,omitempty" yaml:"scaleUpRules,omitempty"`
	ScaleDownRules                *autoscalingv2.HPAScalingRules `json:"scaleDownRules,omitempty" yaml:"scaleDownRules,omitempty"`
	PrescaleWindows               []PrescaleWindow               `json:"prescaleWindows,omitempty" yaml:"prescaleWindows,omitempty"`
}

// Input configures a backtest run over a single HPA timeline trace.
type Input struct {
	Trace      hpaanalysis.TimelineTrace
	Source     string
	Candidates []CandidateSpec
}

// Summary holds aggregated metrics from replaying a trace.
type Summary struct {
	Snapshots                 int     `json:"snapshots" yaml:"snapshots"`
	MetricBackedSnapshots     int     `json:"metricBackedSnapshots" yaml:"metricBackedSnapshots"`
	ScaleEvents               int     `json:"scaleEvents" yaml:"scaleEvents"`
	DirectionFlips            int     `json:"directionFlips" yaml:"directionFlips"`
	PeakReplicas              int32   `json:"peakReplicas" yaml:"peakReplicas"`
	CappedSnapshots           int     `json:"cappedSnapshots" yaml:"cappedSnapshots"`
	CappedDurationSeconds     int64   `json:"cappedDurationSeconds" yaml:"cappedDurationSeconds"`
	UnderProvisionedSnapshots int     `json:"underProvisionedSnapshots" yaml:"underProvisionedSnapshots"`
	UnderProvisionedRatio     float64 `json:"underProvisionedRatio" yaml:"underProvisionedRatio"`
	PodHours                  float64 `json:"podHours" yaml:"podHours"`
	ExtraPodHours             float64 `json:"extraPodHours,omitempty" yaml:"extraPodHours,omitempty"`
}

// CandidateReport summarizes and scores one candidate configuration.
type CandidateReport struct {
	Rank           int               `json:"rank" yaml:"rank"`
	Name           string            `json:"name" yaml:"name"`
	Config         map[string]string `json:"config,omitempty" yaml:"config,omitempty"`
	Summary        Summary           `json:"summary" yaml:"summary"`
	HealthScore    int               `json:"healthScore" yaml:"healthScore"`
	RiskLevel      string            `json:"riskLevel" yaml:"riskLevel"`
	Confidence     string            `json:"confidence" yaml:"confidence"`
	Recommendation string            `json:"recommendation,omitempty" yaml:"recommendation,omitempty"`
}

// Report is the ranked backtest comparison across all candidate configs.
type Report struct {
	Namespace             string            `json:"namespace" yaml:"namespace"`
	Name                  string            `json:"name" yaml:"name"`
	Source                string            `json:"source,omitempty" yaml:"source,omitempty"`
	Snapshots             int               `json:"snapshots" yaml:"snapshots"`
	MetricBackedSnapshots int               `json:"metricBackedSnapshots" yaml:"metricBackedSnapshots"`
	Confidence            string            `json:"confidence" yaml:"confidence"`
	Baseline              Summary           `json:"baseline" yaml:"baseline"`
	BestCandidate         string            `json:"bestCandidate,omitempty" yaml:"bestCandidate,omitempty"`
	Candidates            []CandidateReport `json:"candidates" yaml:"candidates"`
	Limitations           []string          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}
