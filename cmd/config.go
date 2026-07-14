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
	validOutputValues = []string{
		"table", "wide", "json", "jsonl", "yaml", "jsonpath", "template", "gotemplate",
		"markdown", "md", "html", "incident", "prometheus", "junit", "sarif", "github",
	}
	validLangValues = []string{"en", "ja"}

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

// configVersionCurrent is the config-file schema version this build
// understands. Today only "v1" is accepted. Bump (and add a migration) when
// the config shape changes incompatibly; unknown future versions are rejected
// so a v2-unaware binary fails loudly rather than silently misreading a v2
// file. Mirrors the apiVersion gate used by pkg/hpa/policy.
const configVersionCurrent = "v1"

// configFile mirrors the YAML structure accepted by --config.
type configFile struct {
	// ConfigVersion optionally pins the config schema version. When omitted the
	// file is treated as configVersionCurrent for backward compatibility; when
	// present it must equal configVersionCurrent or the file is rejected.
	ConfigVersion string `json:"configVersion" yaml:"configVersion"`

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
	// A present configVersion must match the version this build supports. An
	// absent version is accepted as the current version for backward
	// compatibility with config files written before versioning existed.
	if cfg.ConfigVersion != "" && cfg.ConfigVersion != configVersionCurrent {
		return fmt.Errorf("config configVersion must be %q (or omitted for backward compatibility); got %q", configVersionCurrent, cfg.ConfigVersion)
	}

	checks := []func() error{
		func() error { return validateConfigScalars(cfg) },
		func() error { return validateConfigScores(cfg) },
		func() error { return validateConfigSelectors(cfg) },
		func() error { return validateConfigEnrichment(cfg) },
		func() error { return validateConfigTemplates(cfg.Templates) },
		func() error { return validateConfigHealthWeights(cfg.HealthWeights) },
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigScalars(cfg configFile) error {
	if cfg.ChunkSize != nil && *cfg.ChunkSize < 0 {
		return fmt.Errorf("config chunkSize must be >= 0, got %d", *cfg.ChunkSize)
	}
	if cfg.Events != nil && *cfg.Events < 1 {
		return fmt.Errorf("config events must be greater than zero, got %d", *cfg.Events)
	}
	if cfg.AllNamespaces != nil && *cfg.AllNamespaces && cfg.Namespace != "" {
		return fmt.Errorf("config namespace and allNamespaces=true cannot be used together")
	}
	return nil
}

func validateConfigScores(cfg configFile) error {
	if err := validateScoreField("minScore", cfg.MinScore); err != nil {
		return err
	}
	if err := validateScoreField("maxScore", cfg.MaxScore); err != nil {
		return err
	}
	if err := validateScoreField("healthScore", cfg.HealthScore); err != nil {
		return err
	}
	effectiveMaxScore := cfg.HealthScore
	if effectiveMaxScore == nil {
		effectiveMaxScore = cfg.MaxScore
	}
	if cfg.MinScore != nil && effectiveMaxScore != nil && *cfg.MinScore > *effectiveMaxScore {
		return fmt.Errorf("config minScore cannot be greater than healthScore/maxScore")
	}
	return nil
}

func validateConfigSelectors(cfg configFile) error {
	if !isAcceptedNormalized(strings.ToLower(cfg.Color), validColorValues) {
		return fmt.Errorf("config color must be one of %s; got %q", strings.Join(validColorValues, ", "), cfg.Color)
	}

	// normalizeSelector collapses "go-template" into "gotemplate", so validate
	// against the canonical "gotemplate" spelling used in validOutputValues.
	if !isAcceptedConfigOutput(cfg.Output, cfg.Templates) {
		return fmt.Errorf("config output must be one of %s; got %q", strings.Join(validOutputValues, ", "), cfg.Output)
	}

	if !isAcceptedNormalized(strings.ToLower(cfg.Lang), validLangValues) {
		return fmt.Errorf("config lang must be one of %s; got %q", strings.Join(validLangValues, ", "), cfg.Lang)
	}
	if err := validateMode("config filter", normalizeSelector(cfg.Filter), "", "all", "ok", "error", "limited", "scalinglimited", "issue"); err != nil {
		return err
	}
	return validateMode("config sortBy", normalizeSelector(cfg.SortBy), "", "namespace", "name", "current", "currentreplicas", "desired", "desiredreplicas", "diff", "replicadiff", "difference", "age", "creationtimestamp", "health", "healthscore", "score", "problem", "issue", "min", "minreplicas", "max", "maxreplicas", "target")
}

func validateConfigEnrichment(cfg configFile) error {
	if cfg.Keda != nil {
		if err := validateConfigEnrichmentMode("keda", *cfg.Keda); err != nil {
			return err
		}
	}
	if cfg.Vpa != nil {
		if err := validateConfigEnrichmentMode("vpa", *cfg.Vpa); err != nil {
			return err
		}
	}
	return nil
}

func isAcceptedConfigOutput(value string, templates map[string]outputTemplateConfig) bool {
	if value == "" || isAcceptedNormalized(normalizeSelector(value), validOutputValues) {
		return true
	}
	if _, ok := templates[value]; ok {
		return true
	}
	if name, _, ok := parsePrefixedFormat(value); ok {
		_, exists := templates[name]
		return exists
	}
	return false
}

func validateConfigEnrichmentMode(field, value string) error {
	normalized := normalizeEnrichmentMode(value)
	switch normalized {
	case "", "auto", "on", "off":
		return nil
	default:
		return fmt.Errorf("config %s must be one of auto, on, off; got %q", field, value)
	}
}

func validateConfigTemplates(templates map[string]outputTemplateConfig) error {
	for name, templateCfg := range templates {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("config template name must not be empty")
		}
		if strings.TrimSpace(templateCfg.Template) == "" {
			return fmt.Errorf("config template %q must provide a non-empty template", name)
		}
		switch normalizeSelector(templateCfg.Type) {
		case "", "jsonpath", "template", "gotemplate":
		default:
			return fmt.Errorf("config template %q type must be jsonpath or go-template; got %q", name, templateCfg.Type)
		}
	}
	return nil
}

func validateConfigHealthWeights(weights hpaanalysis.HealthWeights) error {
	opts := &options{}
	opts.HealthWeights = weights
	if err := validateConfiguredHealthWeights(opts); err != nil {
		return fmt.Errorf("config %w", err)
	}
	return nil
}

// validateScoreField validates an optional [0, 100] score field. A nil pointer
// means the field was absent in the config file and is accepted as-is.
func validateScoreField(name string, v *int) error {
	if v == nil {
		return nil
	}
	if *v < 0 || *v > 100 {
		return fmt.Errorf("config %s must be in [0, 100], got %d", name, *v)
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
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return configFile{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return configFile{}, err
	}
	cfg.Output = canonicalConfigOutput(cfg.Output)
	cfg.Color = strings.ToLower(strings.TrimSpace(cfg.Color))
	cfg.Lang = strings.ToLower(strings.TrimSpace(cfg.Lang))
	if cfg.Keda != nil {
		normalized := normalizeEnrichmentMode(*cfg.Keda)
		cfg.Keda = &normalized
	}
	if cfg.Vpa != nil {
		normalized := normalizeEnrichmentMode(*cfg.Vpa)
		cfg.Vpa = &normalized
	}
	return cfg, nil
}

func canonicalConfigOutput(value string) string {
	switch normalizeSelector(value) {
	case "gotemplate":
		return "go-template"
	case "table", "wide", "json", "jsonl", "yaml", "jsonpath", "template",
		"markdown", "md", "html", "incident", "prometheus", "junit", "sarif", "github":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		// Named templates are case-sensitive map keys and must be preserved.
		return value
	}
}

// applyConfigDefaults resolves the config file path, loads the config, and
// applies its values as defaults for any flags the user did not set explicitly.
func applyConfigDefaults(cmd *cobra.Command, opts *options) error {
	path, explicit := opts.Config, opts.Config != ""
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Best-effort: if the home directory is unresolvable (rare: $HOME
			// unset on a stripped-down environment), silently skip config-file
			// loading and fall back to flag/defaults only.
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
// healthWeightSetters maps the normalized --health-weight key name to the
// HealthWeights field it targets. Keeping the mapping in one table (instead of
// a switch with an identical arm per field) makes adding a new weight a
// one-line change and keeps the key spelling authoritative here.
var healthWeightSetters = map[string]func(*hpaanalysis.HealthWeights, int){
	"scalinginactive":     func(w *hpaanalysis.HealthWeights, v int) { w.ScalingInactive = hpaanalysis.IntWeight(v) },
	"unabletoscale":       func(w *hpaanalysis.HealthWeights, v int) { w.UnableToScale = hpaanalysis.IntWeight(v) },
	"scalinglimited":      func(w *hpaanalysis.HealthWeights, v int) { w.ScalingLimited = hpaanalysis.IntWeight(v) },
	"implicitmaxreplicas": func(w *hpaanalysis.HealthWeights, v int) { w.ImplicitMaxReplicas = hpaanalysis.IntWeight(v) },
	"scaledownstabilized": func(w *hpaanalysis.HealthWeights, v int) { w.ScaleDownStabilized = hpaanalysis.IntWeight(v) },
	"atminimumreplicas":   func(w *hpaanalysis.HealthWeights, v int) { w.AtMinimumReplicas = hpaanalysis.IntWeight(v) },
	"kedainactivetrigger": func(w *hpaanalysis.HealthWeights, v int) { w.KEDAInactiveTrigger = hpaanalysis.IntWeight(v) },
	"vpaconflict":         func(w *hpaanalysis.HealthWeights, v int) { w.VPAConflict = hpaanalysis.IntWeight(v) },
}

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
		setter, ok := healthWeightSetters[normalizeSelector(key)]
		if !ok {
			return fmt.Errorf("unknown health weight %q", key)
		}
		setter(&opts.HealthWeights, parsed)
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
