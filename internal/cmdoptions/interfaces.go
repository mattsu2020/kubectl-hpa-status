package cmdoptions

import (
	"io"

	"github.com/mattsui2020/kubectl-hpa-status/internal/kube"
	hpaanalysis "github.com/mattsui2020/kubectl-hpa-status/pkg/hpa"
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
	HealthWeights() hpaanalysis.HealthWeights
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
	HealthScoreRange() (min, max int)
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

func (r *Root) GetClientOverride() kubernetes.Interface { return r.Common.ClientOverride }
func (r *Root) OutputFormat() string                    { return r.Common.Output }
func (r *Root) OutputTemplate() string                  { return r.Common.Template }
func (r *Root) NamedOutputTemplates() map[string]OutputTemplateConfig {
	return r.Common.OutputTemplates
}
func (r *Root) ReportFormat() string { return r.Status.Report }
func (r *Root) ColorMode() string  { return r.Common.Color }
func (r *Root) Language() string     { return r.Common.Lang }
func (r *Root) FeatureFlags() *Features {
	return &r.Status.Features
}
func (r *Root) KEDAMode() string                      { return r.Status.KEDA }
func (r *Root) VPAMode() string                       { return r.Status.VPA }
func (r *Root) SimulateOverrides() []string           { return r.Status.Simulate }
func (r *Root) SimulateMetricOverrides() []string   { return r.Status.SimulateMetric }
func (r *Root) SimulateDurationSeconds() int32      { return r.Status.SimulateDuration }
func (r *Root) HealthWeights() hpaanalysis.HealthWeights { return r.Common.HealthWeights }
func (r *Root) EventLimit() (bool, int) {
	return r.Status.Events.Enabled, r.Status.Events.Limit
}
func (r *Root) DecisionTraceEnabled() bool            { return r.Status.DecisionTrace }
func (r *Root) StructuredDecisionTraceFormat() string { return r.Status.DecisionTraceFormat }
func (r *Root) AssumeControllerProfile() string       { return r.Status.AssumeProfile }
func (r *Root) HPAControllerProfileFile() string      { return r.Status.ControllerProfileFile }
func (r *Root) DebugAnalysis() bool                   { return r.Common.Debug }
func (r *Root) ListSortBy() string                    { return r.List.SortBy }
func (r *Root) ListFilter() string                    { return r.List.Filter }
func (r *Root) HealthScoreRange() (int, int)          { return r.List.HealthScoreMin, r.List.HealthScoreMax }
func (r *Root) ProblemOnly() bool                     { return r.List.Problem }
func (r *Root) SummaryMode() bool                     { return r.List.Summary }
func (r *Root) IsAllNamespaces() bool                 { return r.Common.AllNamespaces }
func (r *Root) LabelSelector() string                 { return r.Common.Selector }
func (r *Root) ListChunkSize() int64                  { return r.Common.ChunkSize }

// Stdin returns the configured input reader.
func (r *Root) Stdin() io.Reader { return r.Common.In }