package cmdoptions

import (
	"io"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"k8s.io/client-go/kubernetes"
)

// KubeClientConfig is the minimal surface commands need to build a client.
type KubeClientConfig interface {
	KubeOptions() kube.Options
	GetClientOverride() kubernetes.Interface
}

// OutputConfig is the minimal surface for format routing.
type OutputConfig interface {
	OutputFormat() string
	OutputTemplate() string
	NamedOutputTemplates() map[string]OutputTemplateConfig
	ReportFormat() string
	ColorMode() string
	Language() string
}

// StatusWorkflowConfig is the enrichment surface consumed by the status pipeline.
type StatusWorkflowConfig interface {
	FeatureFlags() *Features
	KEDAMode() string
	VPAMode() string
	SimulateOverrides() []string
	SimulateMetricOverrides() []string
	SimulateDurationSeconds() int32
	EventLimit() (enabled bool, limit int)
	DecisionTraceEnabled() bool
	StructuredDecisionTraceFormat() string
	AssumeControllerProfile() string
	HPAControllerProfileFile() string
	DebugAnalysis() bool
}

// ListWorkflowConfig is the surface consumed by list and scan commands.
type ListWorkflowConfig interface {
	OutputConfig
	KubeClientConfig
	ListSortBy() string
	ListFilter() string
	HealthScoreRange() (lo, hi int)
	ProblemOnly() bool
	SummaryMode() bool
	IsAllNamespaces() bool
	LabelSelector() string
	ListChunkSize() int64
}

// Ensure Root implements the workflow interfaces.
var (
	_ KubeClientConfig     = (*Root)(nil)
	_ OutputConfig         = (*Root)(nil)
	_ StatusWorkflowConfig = (*Root)(nil)
	_ ListWorkflowConfig   = (*Root)(nil)
)

// GetClientOverride returns the injected Kubernetes client override, if any.
func (r *Root) GetClientOverride() kubernetes.Interface { return r.ClientOverride }

// OutputFormat returns the selected output format string.
func (r *Root) OutputFormat() string { return r.Output }

// OutputTemplate returns the raw Go template string for template output.
func (r *Root) OutputTemplate() string { return r.Template }

// NamedOutputTemplates returns the named output templates from config.
func (r *Root) NamedOutputTemplates() map[string]OutputTemplateConfig {
	return r.OutputTemplates
}

// ReportFormat returns the structured status report format.
func (r *Root) ReportFormat() string { return r.Report }

// ColorMode returns the color preference (auto, always, never).
func (r *Root) ColorMode() string { return r.Color }

// Language returns the configured UI language code.
func (r *Root) Language() string { return r.Lang }

// FeatureFlags returns a pointer to the embedded feature flag group.
func (r *Root) FeatureFlags() *Features {
	return &r.Features
}

// KEDAMode returns the KEDA enrichment mode.
func (r *Root) KEDAMode() string { return r.KEDA }

// VPAMode returns the VPA enrichment mode.
func (r *Root) VPAMode() string { return r.VPA }

// SimulateOverrides returns the replica/metric simulation overrides.
func (r *Root) SimulateOverrides() []string { return r.Simulate }

// SimulateMetricOverrides returns the metric-value simulation overrides.
func (r *Root) SimulateMetricOverrides() []string { return r.SimulateMetric }

// SimulateDurationSeconds returns the simulation horizon in seconds.
func (r *Root) SimulateDurationSeconds() int32 { return r.SimulateDuration }

// EventLimit reports whether event enrichment is enabled and the cap.
func (r *Root) EventLimit() (bool, int) {
	return r.Events.Enabled, r.Events.Limit
}

// DecisionTraceEnabled reports whether structured decision tracing is on.
func (r *Root) DecisionTraceEnabled() bool { return r.DecisionTrace }

// StructuredDecisionTraceFormat returns the decision trace output format.
func (r *Root) StructuredDecisionTraceFormat() string { return r.DecisionTraceFormat }

// AssumeControllerProfile returns the assumed controller profile name.
func (r *Root) AssumeControllerProfile() string { return r.AssumeProfile }

// HPAControllerProfileFile returns the controller profile file path.
func (r *Root) HPAControllerProfileFile() string { return r.ControllerProfileFile }

// DebugAnalysis reports whether verbose analysis logging is enabled.
func (r *Root) DebugAnalysis() bool { return r.Debug }

// ListSortBy returns the list sort key.
func (r *Root) ListSortBy() string { return r.SortBy }

// ListFilter returns the list filter expression.
func (r *Root) ListFilter() string { return r.Filter }

// HealthScoreRange returns the inclusive health-score filter bounds.
func (r *Root) HealthScoreRange() (int, int) { return r.HealthScoreMin, r.HealthScoreMax }

// ProblemOnly reports whether list output is restricted to HPAs with issues.
func (r *Root) ProblemOnly() bool { return r.Problem }

// SummaryMode reports whether list output is collapsed to a summary.
func (r *Root) SummaryMode() bool { return r.Summary }

// IsAllNamespaces reports whether the query spans all namespaces.
func (r *Root) IsAllNamespaces() bool { return r.AllNamespaces }

// LabelSelector returns the Kubernetes label selector string.
func (r *Root) LabelSelector() string { return r.Selector }

// ListChunkSize returns the Kubernetes API pagination page size.
func (r *Root) ListChunkSize() int64 { return r.ChunkSize }

// Stdin returns the configured input reader.
func (r *Root) Stdin() io.Reader { return r.In }
