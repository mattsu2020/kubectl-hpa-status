package cmd

import (
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
)

type flagChangedFunc func(string) bool

func applyStringConfig(value string, flagName string, changed flagChangedFunc, dst *string) {
	if value != "" && !changed(flagName) {
		*dst = value
	}
}

func applyPtrConfig[T any](value *T, flagName string, changed flagChangedFunc, dst *T) {
	if value != nil && !changed(flagName) {
		*dst = *value
	}
}

func applySliceConfig[T any](value []T, flagName string, changed flagChangedFunc, dst *[]T) {
	if len(value) > 0 && !changed(flagName) {
		*dst = value
	}
}

// applyConfig applies config file values only when the corresponding CLI flag
// was not explicitly set by the user. The flag-change check is flag-kind
// agnostic (persistent or local) so that status-local flags registered by
// Phase C still respect --config precedence.
func applyConfig(cmd *cobra.Command, opts *options, cfg configFile) {
	changed := func(name string) bool {
		return flagChanged(cmd, name)
	}

	applyPersistentConfig(opts, cfg, changed)
	applyLocalConfig(opts, cfg, changed)
	applyEventsConfig(opts, cfg, changed)
	applyHealthScoreConfig(opts, cfg, changed)
	applyAdvancedConfig(opts, cfg, changed)
}

func applyPersistentConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	applyStringConfig(cfg.Namespace, "namespace", changed, &opts.namespace)
	applyPtrConfig(cfg.AllNamespaces, "all-namespaces", changed, &opts.allNamespaces)
	applyStringConfig(cfg.Output, "output", changed, &opts.output)
	applyPtrConfig(cfg.Wide, "wide", changed, &opts.wide)
	applyStringConfig(cfg.Selector, "selector", changed, &opts.selector)
	applyStringConfig(cfg.Color, "color", changed, &opts.color)
	applyStringConfig(cfg.Lang, "lang", changed, &opts.lang)
	applyPtrConfig(cfg.Debug, "debug", changed, &opts.debug)
	applyPtrConfig(cfg.Dashboard, "dashboard", changed, &opts.dashboard)
	applyPtrConfig(cfg.ChunkSize, "chunk-size", changed, &opts.chunkSize)
}

func applyLocalConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	applyStringConfig(cfg.SortBy, "sort-by", changed, &opts.sortBy)
	applyStringConfig(cfg.Filter, "filter", changed, &opts.filter)
	applyPtrConfig(cfg.MinScore, "min-score", changed, &opts.healthScoreMin)
}

func applyEventsConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	if changed("events") {
		return
	}

	if cfg.Events != nil {
		opts.events.enabled = true
		opts.events.limit = *cfg.Events
	}
	if cfg.EventsEnabled != nil {
		opts.events.enabled = *cfg.EventsEnabled
	}
}

func applyHealthScoreConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	if changed("max-score") || changed("health-score") {
		return
	}

	switch {
	case cfg.HealthScore != nil:
		opts.healthScoreMax = *cfg.HealthScore
	case cfg.MaxScore != nil:
		opts.healthScoreMax = *cfg.MaxScore
	}
}

func applyAdvancedConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	if len(cfg.Templates) > 0 {
		opts.outputTemplates = cfg.Templates
	}
	if cfg.HealthWeights != (hpaanalysis.HealthWeights{}) {
		opts.healthWeights = cfg.HealthWeights
	}

	applyPtrConfig(cfg.Keda, "keda", changed, &opts.keda)
	applyPtrConfig(cfg.Vpa, "vpa", changed, &opts.vpa)
	applyPtrConfig(cfg.ExplainPods, "explain-pods", changed, &opts.features.explainPods)
	applySliceConfig(cfg.Simulate, "simulate", changed, &opts.simulate)
	applyPtrConfig(cfg.CapacityContext, "capacity-context", changed, &opts.features.capacityContext)
}
