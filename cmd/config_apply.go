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
	applyStringConfig(cfg.Namespace, "namespace", changed, &opts.Namespace)
	applyPtrConfig(cfg.AllNamespaces, "all-namespaces", changed, &opts.AllNamespaces)
	applyStringConfig(cfg.Output, "output", changed, &opts.Output)
	applyPtrConfig(cfg.Wide, "wide", changed, &opts.Wide)
	applyStringConfig(cfg.Selector, "selector", changed, &opts.Selector)
	applyStringConfig(cfg.Color, "color", changed, &opts.Color)
	applyStringConfig(cfg.Lang, "lang", changed, &opts.Lang)
	applyPtrConfig(cfg.Debug, "debug", changed, &opts.Debug)
	applyPtrConfig(cfg.Dashboard, "dashboard", changed, &opts.Dashboard)
	applyPtrConfig(cfg.ChunkSize, "chunk-size", changed, &opts.ChunkSize)
}

func applyLocalConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	applyStringConfig(cfg.SortBy, "sort-by", changed, &opts.SortBy)
	applyStringConfig(cfg.Filter, "filter", changed, &opts.Filter)
	applyPtrConfig(cfg.MinScore, "min-score", changed, &opts.HealthScoreMin)
}

func applyEventsConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	if changed("events") {
		return
	}

	if cfg.Events != nil {
		opts.Events.Enabled = true
		opts.Events.Limit = *cfg.Events
	}
	if cfg.EventsEnabled != nil {
		opts.Events.Enabled = *cfg.EventsEnabled
	}
}

func applyHealthScoreConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	if changed("max-score") || changed("health-score") {
		return
	}

	switch {
	case cfg.HealthScore != nil:
		opts.HealthScoreMax = *cfg.HealthScore
	case cfg.MaxScore != nil:
		opts.HealthScoreMax = *cfg.MaxScore
	}
}

func applyAdvancedConfig(opts *options, cfg configFile, changed flagChangedFunc) {
	if len(cfg.Templates) > 0 {
		opts.OutputTemplates = cfg.Templates
	}
	if cfg.HealthWeights != (hpaanalysis.HealthWeights{}) {
		opts.HealthWeights = cfg.HealthWeights
	}

	applyPtrConfig(cfg.Keda, "keda", changed, &opts.KEDA)
	applyPtrConfig(cfg.Vpa, "vpa", changed, &opts.VPA)
	applyPtrConfig(cfg.ExplainPods, "explain-pods", changed, &opts.ExplainPods)
	applySliceConfig(cfg.Simulate, "simulate", changed, &opts.Simulate)
	applyPtrConfig(cfg.CapacityContext, "capacity-context", changed, &opts.CapacityContext)
}
