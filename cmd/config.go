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
	Keda            *string                         `json:"keda" yaml:"keda"`
	Vpa             *string                         `json:"vpa" yaml:"vpa"`
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
			opts.healthWeights.ScalingInactive = hpaanalysis.IntWeight(parsed)
		case "unabletoscale":
			opts.healthWeights.UnableToScale = hpaanalysis.IntWeight(parsed)
		case "scalinglimited":
			opts.healthWeights.ScalingLimited = hpaanalysis.IntWeight(parsed)
		case "implicitmaxreplicas":
			opts.healthWeights.ImplicitMaxReplicas = hpaanalysis.IntWeight(parsed)
		case "scaledownstabilized":
			opts.healthWeights.ScaleDownStabilized = hpaanalysis.IntWeight(parsed)
		case "atminimumreplicas":
			opts.healthWeights.AtMinimumReplicas = hpaanalysis.IntWeight(parsed)
		case "kedainactivetrigger":
			opts.healthWeights.KEDAInactiveTrigger = hpaanalysis.IntWeight(parsed)
		case "vpaconflict":
			opts.healthWeights.VPAConflict = hpaanalysis.IntWeight(parsed)
		default:
			return fmt.Errorf("unknown health weight %q", key)
		}
	}
	return nil
}

// flagChanged reports whether a flag was explicitly set, regardless of whether
// it is registered as a persistent flag (on the root or any ancestor) or a
// local flag on the running command. This keeps --config value application
// correct when flags move between PersistentFlags and the status subcommand's
// local Flags during the Phase C refactor.
func flagChanged(cmd *cobra.Command, name string) bool {
	// cmd.Flags() is the merged full flagset: it contains local flags owned by
	// cmd itself plus all persistent flags inherited from ancestors. A single
	// Lookup here observes both categories and the Changed bit set by parsing.
	// We walk up so a local flag registered on a parent (e.g. the root command
	// delegating to status) is still observable when cmd is a leaf.
	for current := cmd; current != nil; current = current.Parent() {
		if flag := current.Flags().Lookup(name); flag != nil {
			return flag.Changed
		}
		if flag := current.PersistentFlags().Lookup(name); flag != nil {
			return flag.Changed
		}
	}
	return false
}
