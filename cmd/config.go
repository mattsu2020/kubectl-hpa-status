// Package cmd implements config file loading and flag-to-option binding.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// configFile mirrors the YAML structure accepted by --config.
type configFile struct {
	Namespace       string                          `json:"namespace" yaml:"namespace"`
	AllNamespaces   *bool                           `json:"allNamespaces" yaml:"allNamespaces"`
	Output          string                          `json:"output" yaml:"output"`
	Wide            *bool                           `json:"wide" yaml:"wide"`
	Selector        string                          `json:"selector" yaml:"selector"`
	SortBy          string                          `json:"sortBy" yaml:"sortBy"`
	Filter          string                          `json:"filter" yaml:"filter"`
	MinScore        *int                            `json:"minScore" yaml:"minScore"`
	MaxScore        *int                            `json:"maxScore" yaml:"maxScore"`
	HealthScore     *int                            `json:"healthScore" yaml:"healthScore"`
	Color           string                          `json:"color" yaml:"color"`
	Events          *int                            `json:"events" yaml:"events"`
	EventsEnabled   *bool                           `json:"eventsEnabled" yaml:"eventsEnabled"`
	Lang            string                          `json:"lang" yaml:"lang"`
	Debug           *bool                           `json:"debug" yaml:"debug"`
	Dashboard       *bool                           `json:"dashboard" yaml:"dashboard"`
	ChunkSize       *int64                          `json:"chunkSize" yaml:"chunkSize"`
	Templates       map[string]outputTemplateConfig `json:"templates" yaml:"templates"`
	HealthWeights   hpaanalysis.HealthWeights       `json:"healthWeights" yaml:"healthWeights"`
	Keda            *bool                           `json:"keda" yaml:"keda"`
	Vpa             *bool                           `json:"vpa" yaml:"vpa"`
	ExplainPods     *bool                           `json:"explainPods" yaml:"explainPods"`
	Simulate        []string                        `json:"simulate" yaml:"simulate"`
	CapacityContext *bool                           `json:"capacityContext" yaml:"capacityContext"`
}

// outputTemplateConfig defines a named output template entry in the config file.
type outputTemplateConfig struct {
	Type     string `json:"type" yaml:"type"`
	Template string `json:"template" yaml:"template"`
}

// validateConfig checks config file values for correctness and returns an
// error describing the first invalid field encountered.
func validateConfig(cfg configFile) error {
	if cfg.ChunkSize != nil && *cfg.ChunkSize < 0 {
		return fmt.Errorf("config chunkSize must be >= 0, got %d", *cfg.ChunkSize)
	}

	if cfg.MinScore != nil && (*cfg.MinScore < 0 || *cfg.MinScore > 100) {
		return fmt.Errorf("config minScore must be in [0, 100], got %d", *cfg.MinScore)
	}
	if cfg.MaxScore != nil && (*cfg.MaxScore < 0 || *cfg.MaxScore > 100) {
		return fmt.Errorf("config maxScore must be in [0, 100], got %d", *cfg.MaxScore)
	}
	if cfg.HealthScore != nil && (*cfg.HealthScore < 0 || *cfg.HealthScore > 100) {
		return fmt.Errorf("config healthScore must be in [0, 100], got %d", *cfg.HealthScore)
	}

	switch strings.ToLower(cfg.Color) {
	case "", "auto", "always", "never":
	default:
		return fmt.Errorf("config color must be one of auto, always, never; got %q", cfg.Color)
	}

	switch normalizeSelector(cfg.Output) {
	case "", "table", "wide", "json", "yaml", "jsonpath", "template", "gotemplate":
	default:
		return fmt.Errorf("config output must be one of table, wide, json, yaml, jsonpath, template; got %q", cfg.Output)
	}

	switch strings.ToLower(cfg.Lang) {
	case "", "en", "ja":
	default:
		return fmt.Errorf("config lang must be one of en, ja; got %q", cfg.Lang)
	}

	return nil
}

// loadConfigFile reads and parses a YAML config file at the given path.
func loadConfigFile(path string) (configFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is from user --config flag, not arbitrary input
	if err != nil {
		return configFile{}, err
	}
	var cfg configFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return configFile{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return configFile{}, err
	}
	return cfg, nil
}

// applyConfig applies config file values only when the corresponding CLI flag
// was not explicitly set by the user.
func applyConfig(cmd *cobra.Command, opts *options, cfg configFile) {
	if cfg.Namespace != "" && !persistentFlagChanged(cmd, "namespace") {
		opts.namespace = cfg.Namespace
	}
	if cfg.AllNamespaces != nil && !persistentFlagChanged(cmd, "all-namespaces") {
		opts.allNamespaces = *cfg.AllNamespaces
	}
	if cfg.Output != "" && !persistentFlagChanged(cmd, "output") {
		opts.output = cfg.Output
	}
	if cfg.Wide != nil && !persistentFlagChanged(cmd, "wide") {
		opts.wide = *cfg.Wide
	}
	if cfg.Selector != "" && !persistentFlagChanged(cmd, "selector") {
		opts.selector = cfg.Selector
	}
	if cfg.Color != "" && !persistentFlagChanged(cmd, "color") {
		opts.color = cfg.Color
	}
	if cfg.Lang != "" && !persistentFlagChanged(cmd, "lang") {
		opts.lang = cfg.Lang
	}
	if cfg.Debug != nil && !persistentFlagChanged(cmd, "debug") {
		opts.debug = *cfg.Debug
	}
	if cfg.Dashboard != nil && !persistentFlagChanged(cmd, "dashboard") {
		opts.dashboard = *cfg.Dashboard
	}
	if cfg.ChunkSize != nil && !persistentFlagChanged(cmd, "chunk-size") {
		opts.chunkSize = *cfg.ChunkSize
	}
	if len(cfg.Templates) > 0 {
		opts.outputTemplates = cfg.Templates
	}
	if cfg.Events != nil && !persistentFlagChanged(cmd, "events") {
		opts.events.enabled = true
		opts.events.limit = *cfg.Events
	}
	if cfg.EventsEnabled != nil && !persistentFlagChanged(cmd, "events") {
		opts.events.enabled = *cfg.EventsEnabled
	}
	if cfg.SortBy != "" && !localFlagChanged(cmd, "sort-by") {
		opts.sortBy = cfg.SortBy
	}
	if cfg.Filter != "" && !localFlagChanged(cmd, "filter") {
		opts.filter = cfg.Filter
	}
	if cfg.MinScore != nil && !localFlagChanged(cmd, "min-score") {
		opts.healthScoreMin = *cfg.MinScore
	}
	if cfg.MaxScore != nil && !localFlagChanged(cmd, "max-score") && !localFlagChanged(cmd, "health-score") {
		opts.healthScoreMax = *cfg.MaxScore
	}
	if cfg.HealthScore != nil && !localFlagChanged(cmd, "health-score") && !localFlagChanged(cmd, "max-score") {
		opts.healthScoreMax = *cfg.HealthScore
	}
	if cfg.HealthWeights != (hpaanalysis.HealthWeights{}) {
		opts.healthWeights = cfg.HealthWeights
	}
	if cfg.Keda != nil && !persistentFlagChanged(cmd, "keda") {
		opts.keda = *cfg.Keda
	}
	if cfg.Vpa != nil && !persistentFlagChanged(cmd, "vpa") {
		opts.vpa = *cfg.Vpa
	}
	if cfg.ExplainPods != nil && !persistentFlagChanged(cmd, "explain-pods") {
		opts.explainPods = *cfg.ExplainPods
	}
	if len(cfg.Simulate) > 0 && !persistentFlagChanged(cmd, "simulate") {
		opts.simulate = cfg.Simulate
	}
	if cfg.CapacityContext != nil && !persistentFlagChanged(cmd, "capacity-context") {
		opts.capacityContext = *cfg.CapacityContext
	}
}

// applyConfigDefaults resolves the config file path, loads the config, and
// applies its values as defaults for any flags the user did not set explicitly.
func applyConfigDefaults(cmd *cobra.Command, opts *options) error {
	path, explicit := opts.config, opts.config != ""
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".kube", "hpa-status.yaml")
	}

	cfg, err := loadConfigFile(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to load config %s: %w", path, err)
	}
	applyConfig(cmd, opts, cfg)
	return nil
}

// applyHealthWeightOverrides parses --health-weight name=value flags and
// applies the overrides to the options healthWeights struct.
func applyHealthWeightOverrides(opts *options) error {
	for _, override := range opts.healthWeightOverrides {
		key, value, ok := strings.Cut(override, "=")
		if !ok {
			return fmt.Errorf("invalid --health-weight %q; expected name=value", override)
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return fmt.Errorf("invalid --health-weight %q; value must be a non-negative integer", override)
		}
		switch normalizeSelector(key) {
		case "scalinginactive":
			opts.healthWeights.ScalingInactive = parsed
		case "unabletoscale":
			opts.healthWeights.UnableToScale = parsed
		case "scalinglimited":
			opts.healthWeights.ScalingLimited = parsed
		case "implicitmaxreplicas":
			opts.healthWeights.ImplicitMaxReplicas = parsed
		case "scaledownstabilized":
			opts.healthWeights.ScaleDownStabilized = parsed
		case "atminimumreplicas":
			opts.healthWeights.AtMinimumReplicas = parsed
		case "kedainactivetrigger":
			opts.healthWeights.KEDAInactiveTrigger = parsed
		case "vpaconflict":
			opts.healthWeights.VPAConflict = parsed
		default:
			return fmt.Errorf("unknown health weight %q", key)
		}
	}
	return nil
}

// persistentFlagChanged reports whether a persistent flag was explicitly set.
func persistentFlagChanged(cmd *cobra.Command, name string) bool {
	root := cmd.Root()
	if root == nil {
		return false
	}
	flag := root.PersistentFlags().Lookup(name)
	return flag != nil && flag.Changed
}

// localFlagChanged reports whether a local flag was explicitly set on the
// command or any of its parents.
func localFlagChanged(cmd *cobra.Command, name string) bool {
	for current := cmd; current != nil; current = current.Parent() {
		flag := current.Flags().Lookup(name)
		if flag != nil {
			return flag.Changed
		}
	}
	return false
}
