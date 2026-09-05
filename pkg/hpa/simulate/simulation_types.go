package simulate

import (
	"errors"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// Metric holds a single metric's current state for analysis and simulation.
// This is a local copy to avoid import cycles with the hpa root package.
type Metric struct {
	Type            string   `json:"type" yaml:"type"`
	Name            string   `json:"name,omitempty" yaml:"name,omitempty"`
	Selector        string   `json:"selector,omitempty" yaml:"selector,omitempty"`
	Current         string   `json:"current" yaml:"current"`
	Target          string   `json:"target" yaml:"target"`
	CurrentReplicas int32    `json:"currentReplicas,omitempty" yaml:"currentReplicas,omitempty"`
	AverageValue    string   `json:"averageValue,omitempty" yaml:"averageValue,omitempty"`
	Note            string   `json:"note,omitempty" yaml:"note,omitempty"`
	Ratio           *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
}

// HealthWeights configures health score calculation penalties.
// This is a local copy to avoid import cycles with the hpa root package.
type HealthWeights struct {
	// Limited is the penalty for HPA being at maxReplicas (default: 40).
	Limited int `json:"limited,omitempty" yaml:"limited,omitempty"`
	// NotReady is the penalty for pods not being ready (default: 10).
	NotReady int `json:"notReady,omitempty" yaml:"notReady,omitempty"`
	// Falling is the penalty for trending down (default: 5).
	Falling int `json:"falling,omitempty" yaml:"falling,omitempty"`
	// MetricUnavailable is the penalty for metric fetch failures (default: 20).
	MetricUnavailable int `json:"metricUnavailable,omitempty" yaml:"metricUnavailable,omitempty"`
}

// HealthWeightsFrom converts the hpa root package's pointer-based penalty
// weights into this package's flat form. A nil pointer selects the default
// penalty. Note the flat form cannot represent "explicitly disable" (*int 0)
// — that distinction only exists on the AnalyzeWithOptions path.
func HealthWeightsFrom(limited, notReady, falling *int) HealthWeights {
	deref := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	return HealthWeights{Limited: deref(limited), NotReady: deref(notReady), Falling: deref(falling)}
}

// Analysis holds the full HPA analysis result.
// This is a local copy to avoid import cycles with the hpa root package.
type Analysis struct {
	Namespace   string
	Name        string
	Target      string
	Current     int32
	Desired     int32
	Min         int32
	Max         int32
	Health      string
	HealthScore int
	Summary     string
	Metrics     []Metric
	Conditions  []Condition
}

// Condition represents an HPA condition.
// This is a local copy to avoid import cycles with the hpa root package.
type Condition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// AnalysisOptions configures analysis behavior.
type AnalysisOptions struct {
	// HealthWeights applies custom penalties to health score calculation.
	HealthWeights HealthWeights
	// IncludeMetrics fetches and interprets current metrics (default: true).
	IncludeMetrics bool
	// IncludeConditions fetches and interprets HPA conditions (default: true).
	IncludeConditions bool
	// EnableObservations enables additional observation-based enrichment.
	EnableObservations bool
	// ForTesting suppresses certain validations for testing.
	ForTesting bool
}

// ErrNilHPA is the sentinel error for nil HPA inputs.
var ErrNilHPA = errors.New("HPA must not be nil")

// ErrMetricNotFound is returned when a simulation override references a
// metric that does not exist in the HPA spec.
var ErrMetricNotFound = errors.New("metric not found in HPA spec")

// ErrMetricAmbiguous is returned when a name-only metric reference matches
// multiple metrics and the identity cannot be uniquely determined.
var ErrMetricAmbiguous = errors.New("metric name is ambiguous")

// Health state constants for testing
const (
	HealthOK         = "OK"
	HealthWarning    = "WARNING"
	HealthError      = "ERROR"
	HealthLimited    = "LIMITED"
	HealthStabilized = "STABILIZED"
)

// ErrDependencyMissing is returned when a simulate entry point runs before
// the hpa root package installed its dependencies. Importing
// github.com/mattsu2020/kubectl-hpa-status/pkg/hpa (a blank import suffices)
// installs them via that package's init; SetAnalyzeFunc and
// SetMetricImpactRatioFunc allow explicit registration instead.
var ErrDependencyMissing = errors.New("simulate: dependency not registered")

// -------------------------------------------------------------------
// Function pointer injections for hpa root package helpers that cannot move
// to a lower package (the full analysis pipeline and the per-type metric
// handler registry). Shared pure computation lives in pkg/hpa/internal/*
// and is called directly; only these two dependencies are injected.
// -------------------------------------------------------------------

// analyzeFunc is a function pointer type for HPA analysis.
// This allows injection of the hpa root package's AnalyzeWithOptions without import cycles.
type analyzeFunc func(hpa *autoscalingv2.HorizontalPodAutoscaler, includeMetrics bool, opts AnalysisOptions) Analysis

// metricImpactRatioFunc returns the display name and impact ratio of one
// current metric.
type metricImpactRatioFunc func(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64)

// Function pointer variables
var (
	analyzeFuncInstance       analyzeFunc
	metricImpactRatioFuncImpl metricImpactRatioFunc
)

// SetAnalyzeFunc sets the analysis function for simulation.
// This is called from the hpa root package to inject the AnalyzeWithOptions dependency.
func SetAnalyzeFunc(fn analyzeFunc) {
	analyzeFuncInstance = fn
}

// SetMetricImpactRatioFunc sets the metricImpactRatio function.
func SetMetricImpactRatioFunc(fn metricImpactRatioFunc) {
	metricImpactRatioFuncImpl = fn
}

// AnalysisFuncInvoker invokes the injected analysis function. It returns
// ErrDependencyMissing instead of panicking when the hpa root package has not
// been linked (and no SetAnalyzeFunc registration happened).
func AnalysisFuncInvoker(hpa *autoscalingv2.HorizontalPodAutoscaler, includeMetrics bool, opts AnalysisOptions) (Analysis, error) {
	if analyzeFuncInstance == nil {
		return Analysis{}, fmt.Errorf("%w: analysis (import github.com/mattsu2020/kubectl-hpa-status/pkg/hpa or call SetAnalyzeFunc)", ErrDependencyMissing)
	}
	return analyzeFuncInstance(hpa, includeMetrics, opts), nil
}

func metricImpactRatioInvoker(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) (string, *float64, error) {
	if metricImpactRatioFuncImpl == nil {
		return "", nil, fmt.Errorf("%w: metricImpactRatio (import github.com/mattsu2020/kubectl-hpa-status/pkg/hpa or call SetMetricImpactRatioFunc)", ErrDependencyMissing)
	}
	name, ratio := metricImpactRatioFuncImpl(hpa, metric)
	return name, ratio, nil
}

// SimulationResult holds the before/after comparison of an HPA simulation.
type SimulationResult struct {
	Parameter            string             `json:"parameter" yaml:"parameter"`
	OriginalValue        string             `json:"originalValue" yaml:"originalValue"`
	SimulatedValue       string             `json:"simulatedValue" yaml:"simulatedValue"`
	Before               SimulationState    `json:"before" yaml:"before"`
	After                SimulationState    `json:"after" yaml:"after"`
	Confidence           string             `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	RiskAssessment       string             `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
	Interpretation       []string           `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	MetricSimulations    []MetricSimulation `json:"metricSimulations,omitempty" yaml:"metricSimulations,omitempty"`
	TimeSeriesProjection []ProjectedState   `json:"timeSeriesProjection,omitempty" yaml:"timeSeriesProjection,omitempty"`
	RiskWarnings         []string           `json:"riskWarnings,omitempty" yaml:"riskWarnings,omitempty"`
}

// SimulationState is a snapshot of key analysis fields for before/after comparison.
type SimulationState struct {
	DesiredReplicas int32    `json:"desiredReplicas" yaml:"desiredReplicas"`
	Health          string   `json:"health" yaml:"health"`
	HealthScore     int      `json:"healthScore" yaml:"healthScore"`
	Summary         string   `json:"summary" yaml:"summary"`
	ScalingLimited  bool     `json:"scalingLimited" yaml:"scalingLimited"`
	Metrics         []Metric `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// ProjectedState holds a single point in a time-series projection showing
// estimated replica count at a given time offset.
type ProjectedState struct {
	TimeOffset           int32   `json:"timeOffset" yaml:"timeOffset"`
	ProjectedReplicas    int32   `json:"projectedReplicas" yaml:"projectedReplicas"`
	ProjectedMetricRatio float64 `json:"projectedMetricRatio,omitempty" yaml:"projectedMetricRatio,omitempty"`
}

// SimulationExtendedOptions configures extended simulation with time-series
// projection and additional parameter overrides.
type SimulationExtendedOptions struct {
	DurationSeconds int32 `json:"durationSeconds" yaml:"durationSeconds"`
	StepSeconds     int32 `json:"stepSeconds" yaml:"stepSeconds"`
}

// MetricSimulation holds the result of simulating a metric value change.
type MetricSimulation struct {
	// MetricName is the name of the simulated metric.
	MetricName string `json:"metricName" yaml:"metricName"`
	// OriginalValue is the current metric value before simulation.
	OriginalValue string `json:"originalValue" yaml:"originalValue"`
	// SimulatedValue is the simulated metric value.
	SimulatedValue string `json:"simulatedValue" yaml:"simulatedValue"`
	// ProjectedRatio is the estimated ratio after simulation.
	ProjectedRatio *float64 `json:"projectedRatio,omitempty" yaml:"projectedRatio,omitempty"`
	// ProjectedReplicas is the estimated desired replica count.
	ProjectedReplicas int32 `json:"projectedReplicas" yaml:"projectedReplicas"`
	// ToleranceImpact describes whether tolerance would suppress this change.
	ToleranceImpact string `json:"toleranceImpact,omitempty" yaml:"toleranceImpact,omitempty"`
	// StabilizationImpact describes whether stabilization would delay this change.
	StabilizationImpact string `json:"stabilizationImpact,omitempty" yaml:"stabilizationImpact,omitempty"`
	// RiskAssessment for this specific metric simulation.
	RiskAssessment string `json:"riskAssessment,omitempty" yaml:"riskAssessment,omitempty"`
}
