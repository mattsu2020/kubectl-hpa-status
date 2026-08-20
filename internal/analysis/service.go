// Package analysis provides application-level orchestration for turning HPA
// observations into finalized analysis results. It deliberately owns no
// Kubernetes clients: callers collect observations/enrichment once and pass
// them in, which keeps CLI, TUI, and tests on the same deterministic path.
package analysis

import (
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// Options controls the pure analysis pass shared by list-like workflows.
type Options struct {
	IncludeInterpretation bool
	Debug                 bool
	HealthWeights         hpaanalysis.HealthWeights
}

// ItemEnrichment contains already-observed optional data for one HPA.
// Warnings distinguish an unavailable observation from an observed absence.
type ItemEnrichment struct {
	KEDA     *hpakeda.Analysis
	VPA      *hpavpa.ConflictInfo
	Warnings []string
}

// BatchEnrichment contains batched observations keyed by "namespace/name".
// Warning entries are keyed by namespace because a failed list call affects
// every HPA in that namespace.
type BatchEnrichment struct {
	KEDA     map[string]*hpakeda.Analysis
	VPA      map[string]*hpavpa.ConflictInfo
	Warnings map[string][]string
}

// Result is the canonical finalized representation used by list and TUI.
type Result struct {
	Key       string
	Analysis  hpaanalysis.Analysis
	Canonical hpaanalysis.GroupedAnalysis
	ListItem  hpaanalysis.ListItem
	Report    hpaanalysis.StatusReport
}

// AnalyzeOne runs the complete deterministic analysis path for one HPA.
func AnalyzeOne(hpa *autoscalingv2.HorizontalPodAutoscaler, opts Options, enrichment ItemEnrichment) Result {
	analysis := hpaanalysis.AnalyzeWithOptions(hpa, opts.IncludeInterpretation, hpaanalysis.AnalysisOptions{
		Debug:         opts.Debug,
		HealthWeights: opts.HealthWeights,
	})
	builder := hpaanalysis.NewAnalysisBuilder(analysis).AddEnrichment(
		enrichment.KEDA, enrichment.VPA, enrichment.Warnings, opts.HealthWeights,
	)
	analysis, report := builder.Build()

	key := analysis.Namespace() + "/" + analysis.Name()
	return Result{
		Key:       key,
		Analysis:  analysis,
		Canonical: report.CanonicalAnalysis(),
		ListItem:  hpaanalysis.NewListItem(analysis),
		Report:    report,
	}
}

// AnalyzeBatch analyzes HPAs in input order while reusing batched enrichment.
func AnalyzeBatch(hpas []autoscalingv2.HorizontalPodAutoscaler, opts Options, enrichment BatchEnrichment) []Result {
	results := make([]Result, 0, len(hpas))
	for i := range hpas {
		hpa := &hpas[i]
		key := hpa.Namespace + "/" + hpa.Name
		item := ItemEnrichment{
			KEDA:     enrichment.KEDA[key],
			VPA:      enrichment.VPA[key],
			Warnings: cloneStrings(enrichment.Warnings[hpa.Namespace]),
		}
		results = append(results, AnalyzeOne(hpa, opts, item))
	}
	return results
}

// MergeNamespaceWarnings combines warning maps without aliasing the inputs.
func MergeNamespaceWarnings(sources ...map[string][]string) map[string][]string {
	var merged map[string][]string
	for _, source := range sources {
		for namespace, warnings := range source {
			if len(warnings) == 0 {
				continue
			}
			if merged == nil {
				merged = make(map[string][]string)
			}
			merged[namespace] = append(merged[namespace], warnings...)
		}
	}
	return merged
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}
