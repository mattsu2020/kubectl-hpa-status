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

// Accepted config-file values. These are the single source of truth shared by
// validateConfig below and the cobra flag descriptions in root_flags.go. Output
// values are kept in canonical (normalized) form because validateConfig
// normalizes user input before comparison.
var (
	validColorValues  = []string{"auto", "always", "never"}
	validOutputValues = []string{"table", "wide", "json", "jsonl", "yaml", "jsonpath", "template", "gotemplate"}
	validLangValues   = []string{"en", "ja"}

	// outputFlagDisplayValues mirrors validOutputValues but keeps the
	// kubectl-conventional "go-template" spelling for the --help string.
	outputFlagDisplayValues = []string{"table", "wide", "json", "jsonl", "yaml", "jsonpath", "go-template"}
)

// isAcceptedNormalized reports whether value (already normalized) is one of the
// accepted options, treating the empty string as "unset/allowed".
func isAcceptedNormalized(value string, accepted []string) bool {
	if value == "" {
		return true
	}
	for _, a := range accepted {
		if value == a {
			return true
		}
	}
	return false
}

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

	if !isAcceptedNormalized(strings.ToLower(cfg.Color), validColorValues) {
		return fmt.Errorf("config color must be one of %s; got %q", strings.Join(validColorValues, ", "), cfg.Color)
	}

	// normalizeSelector collapses "go-template" into "gotemplate", so validate
	// against the canonical "gotemplate" spelling used in validOutputValues.
	if !isAcceptedNormalized(normalizeSelector(cfg.Output), validOutputValues) {
		return fmt.Errorf("config output must be one of %s; got %q", strings.Join(validOutputValues, ", "), cfg.Output)
	}

	if !isAcceptedNormalized(strings.ToLower(cfg.Lang), validLangValues) {
		return fmt.Errorf("config lang must be one of %s; got %q", strings.Join(validLangValues, ", "), cfg.Lang)
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
	path, explicit := opts.Config, opts.Config != ""
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
	for _, override := range opts.HealthWeightOverrides {
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
			opts.HealthWeights.ScalingInactive = hpaanalysis.IntWeight(parsed)
		case "unabletoscale":
			opts.HealthWeights.UnableToScale = hpaanalysis.IntWeight(parsed)
		case "scalinglimited":
			opts.HealthWeights.ScalingLimited = hpaanalysis.IntWeight(parsed)
		case "implicitmaxreplicas":
			opts.HealthWeights.ImplicitMaxReplicas = hpaanalysis.IntWeight(parsed)
		case "scaledownstabilized":
			opts.HealthWeights.ScaleDownStabilized = hpaanalysis.IntWeight(parsed)
		case "atminimumreplicas":
			opts.HealthWeights.AtMinimumReplicas = hpaanalysis.IntWeight(parsed)
		case "kedainactivetrigger":
			opts.HealthWeights.KEDAInactiveTrigger = hpaanalysis.IntWeight(parsed)
		case "vpaconflict":
			opts.HealthWeights.VPAConflict = hpaanalysis.IntWeight(parsed)
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
