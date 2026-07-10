package cmd

import (
	"context"

	hpakeda "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/keda"
	hpavpa "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/vpa"

	"github.com/mattsu2020/kubectl-hpa-status/internal/enrichment"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// enrichmentContext is a thin wrapper around internal/enrichment.Context
// that bridges the CLI options layer to the enrichment package.
type enrichmentContext = enrichment.Context

// newEnrichmentContext creates an enrichment context from CLI options.
func newEnrichmentContext(ctx context.Context, opts *options) *enrichment.Context {
	return enrichment.NewContext(ctx, enrichment.Config{
		Kube: opts.KubeOptions(),
		KEDA: opts.KEDA,
		VPA:  opts.VPA,
	})
}

// enrichReport applies KEDA and VPA enrichment to a StatusReport.
func enrichReport(ctx context.Context, ec *enrichment.Context, hpa *autoscalingv2.HorizontalPodAutoscaler, report *hpaanalysis.StatusReport, weights hpaanalysis.HealthWeights) {
	enrichment.EnrichReport(ctx, ec, hpa, report, weights)
}

// enrichListKEDA performs batched KEDA enrichment for multiple HPAs.
// Returns the per-HPA analysis map plus per-namespace list-failure warnings
// (namespace → messages).
func enrichListKEDA(ctx context.Context, ec *enrichment.Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpakeda.Analysis, map[string][]string) {
	return enrichment.BatchKEDA(ctx, ec, hpas)
}

// enrichListVPA performs batched VPA enrichment for multiple HPAs.
// Returns the per-HPA conflict map plus per-namespace list-failure warnings
// (namespace → messages).
func enrichListVPA(ctx context.Context, ec *enrichment.Context, hpas []autoscalingv2.HorizontalPodAutoscaler) (map[string]*hpavpa.ConflictInfo, map[string][]string) {
	return enrichment.BatchVPA(ctx, ec, hpas)
}
