package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// applyStatusDepthDefaults adds reads promised by the explicit status depth
// tier without changing the HPA-only baseline. An explicit --events value or
// config entry always wins, including events: false.
func applyStatusDepthDefaults(cmd *cobra.Command, opts *options) {
	if cmd == nil || opts == nil {
		return
	}
	if flagChanged(cmd, "events") {
		opts.EventsConfigured = true
	}
	if cmd.Name() == "status" && opts.Explain && !opts.EventsConfigured {
		opts.Events = EventOption{Enabled: true, Limit: 5}
	}
}

// validateEffectiveOptions validates the post-config, post-normalization
// option set before any command opens a Kubernetes client or mutates output.
// This turns malformed explicit configuration into a deterministic CLI error
// instead of silently falling back to unrelated defaults.
func validateEffectiveOptions(cmd *cobra.Command, opts *options) error {
	if opts == nil {
		return fmt.Errorf("internal error: command options are nil")
	}
	normalizeEffectiveEnums(opts)
	if cmd != nil && flagChanged(cmd, "health-score") {
		opts.HealthScoreMaxConfigured = true
	}

	if !isAcceptedNormalized(opts.Color, validColorValues) {
		return fmt.Errorf("--color must be one of %s; got %q", strings.Join(validColorValues, ", "), opts.Color)
	}
	if !isAcceptedNormalized(opts.Lang, validLangValues) {
		return fmt.Errorf("--lang must be one of %s; got %q", strings.Join(validLangValues, ", "), opts.Lang)
	}
	if opts.AllNamespaces && opts.Namespace != "" {
		return fmt.Errorf("--namespace and --all-namespaces cannot be used together")
	}
	if opts.ChunkSize < 0 {
		return fmt.Errorf("--chunk-size must be >= 0, got %d", opts.ChunkSize)
	}
	if opts.Concurrency < 1 {
		return fmt.Errorf("--concurrency must be greater than zero, got %d", opts.Concurrency)
	}
	if opts.QPS < 0 {
		return fmt.Errorf("--qps must be >= 0, got %g", opts.QPS)
	}
	if opts.Burst < 0 {
		return fmt.Errorf("--burst must be >= 0, got %d", opts.Burst)
	}
	if opts.RequestTimeout < 0 {
		return fmt.Errorf("--request-timeout must be >= 0, got %s", opts.RequestTimeout)
	}
	if opts.WatchInterval <= 0 {
		return fmt.Errorf("--interval must be greater than zero, got %s", opts.WatchInterval)
	}
	if opts.WatchTimeout < 0 {
		return fmt.Errorf("--timeout must be >= 0, got %s", opts.WatchTimeout)
	}
	if opts.TrendSince < 0 || opts.TrendRetain < 0 {
		return fmt.Errorf("--trend-since and --trend-retain must be >= 0")
	}
	if opts.SimulateDuration < 0 {
		return fmt.Errorf("--simulate-duration must be >= 0, got %d", opts.SimulateDuration)
	}
	if opts.TargetMax < 0 {
		return fmt.Errorf("--target-max must be >= 0, got %d", opts.TargetMax)
	}
	if opts.Events.Enabled && opts.Events.Limit < 1 {
		return fmt.Errorf("--events limit must be greater than zero")
	}

	if err := validateMode("--keda", opts.KEDA, "", "auto", "on", "off"); err != nil {
		return err
	}
	if err := validateMode("--vpa", opts.VPA, "", "auto", "on", "off"); err != nil {
		return err
	}
	if err := validateMode("--policy-guard-mode", opts.PolicyGuardMode, "block", "warn"); err != nil {
		return err
	}
	if err := validateMode("--format", opts.Format, "", "structured"); err != nil {
		return err
	}
	if err := validateMode("--decision-trace-format", opts.DecisionTraceFormat, "", "text", "json", "yaml"); err != nil {
		return err
	}
	if err := validateMode("--report", opts.Report, "", "markdown", "md", "html", "incident", "junit", "sarif"); err != nil {
		return err
	}
	if err := validateMode("--export", opts.Export, "", "yaml", "kustomize", "helm-values", "directory"); err != nil {
		return err
	}
	if err := validateCommandOutputModes(cmd, opts); err != nil {
		return err
	}
	if err := validateConfiguredHealthWeights(opts); err != nil {
		return err
	}
	if err := validateOutputOption(cmd, opts); err != nil {
		return err
	}
	if err := validateApplyOutputContract(opts); err != nil {
		return err
	}

	if cmd != nil && (cmd.Name() == "list" || cmd.Name() == "scan") {
		if err := validateListOptions(opts); err != nil {
			return err
		}
		if cmd.Name() == "list" && opts.Apply {
			filter := opts.Filter
			if opts.Problem && filter == "" {
				filter = "issue"
			}
			if err := validateListApply(opts, filter); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCommandOutputModes(cmd *cobra.Command, opts *options) error {
	if cmd == nil {
		return nil
	}
	isList := cmd.Name() == "list" || cmd.Name() == "scan"
	if isList && opts.Export != "" && opts.Export != "directory" {
		return fmt.Errorf("%s supports --export=directory only; use status for --export=%s", cmd.Name(), opts.Export)
	}
	if !isList && opts.Export == "directory" {
		return fmt.Errorf("--export=directory is supported only by list and scan")
	}
	if (opts.Report == "junit" || opts.Report == "sarif") && !isList {
		return fmt.Errorf("--report=%s is supported only by list and scan", opts.Report)
	}
	if opts.Report == "incident" && isList {
		return fmt.Errorf("--report=incident requires a single or multi-name status workflow; use markdown or html for list/scan")
	}
	return nil
}

func validateOutputOption(cmd *cobra.Command, opts *options) error {
	if opts.Output == "" {
		return nil
	}
	// record keeps the documented -o FILE compatibility. Values recognized as
	// formats still pass through the normal validation below.
	if cmd != nil && cmd.Name() == "record" && !isKnownOutputFormat(opts.Output) {
		return nil
	}

	format, templateStr := selectOutputFromOptions(opts)
	format = normalizeOutputFormat(format)
	switch format {
	case "", "table", "wide", "ja", "json", "jsonl", "yaml", "markdown", "md",
		"html", "incident", "prometheus", "junit", "sarif", "github":
		return nil
	case "jsonpath", "go-template", "template":
		if strings.TrimSpace(templateStr) == "" {
			return fmt.Errorf("--output=%s requires --template or a configured named template", format)
		}
		return nil
	default:
		if expression, _, ok := parsePrefixedFormat(format); ok {
			if strings.TrimSpace(expression) == "" {
				return fmt.Errorf("--output=%q has an empty template expression", opts.Output)
			}
			return nil
		}
		return fmt.Errorf("unsupported --output format %q", opts.Output)
	}
}

func normalizeEffectiveEnums(opts *options) {
	opts.Color = strings.ToLower(strings.TrimSpace(opts.Color))
	opts.Lang = strings.ToLower(strings.TrimSpace(opts.Lang))
	opts.KEDA = normalizeEnrichmentMode(opts.KEDA)
	opts.VPA = normalizeEnrichmentMode(opts.VPA)
	opts.PolicyGuardMode = strings.ToLower(strings.TrimSpace(opts.PolicyGuardMode))
	opts.Format = strings.ToLower(strings.TrimSpace(opts.Format))
	opts.DecisionTraceFormat = strings.ToLower(strings.TrimSpace(opts.DecisionTraceFormat))
	opts.Report = strings.ToLower(strings.TrimSpace(opts.Report))
	opts.Export = strings.ToLower(strings.TrimSpace(opts.Export))
	if normalizeOutputFormat(opts.Output) == "gotemplate" {
		opts.Output = "go-template"
	}
}

func normalizeEnrichmentMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return "on"
	case "false", "0":
		return "off"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func validateMode(flag, value string, accepted ...string) error {
	for _, candidate := range accepted {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s; got %q", flag, strings.Join(accepted, ", "), value)
}

func validateConfiguredHealthWeights(opts *options) error {
	weights := []struct {
		name  string
		value *int
	}{
		{"scalingInactive", opts.HealthWeights.ScalingInactive},
		{"unableToScale", opts.HealthWeights.UnableToScale},
		{"scalingLimited", opts.HealthWeights.ScalingLimited},
		{"implicitMaxReplicas", opts.HealthWeights.ImplicitMaxReplicas},
		{"scaleDownStabilized", opts.HealthWeights.ScaleDownStabilized},
		{"atMinimumReplicas", opts.HealthWeights.AtMinimumReplicas},
		{"kedaInactiveTrigger", opts.HealthWeights.KEDAInactiveTrigger},
		{"vpaConflict", opts.HealthWeights.VPAConflict},
		{"churn", opts.HealthWeights.Churn},
	}
	for _, weight := range weights {
		if weight.value != nil && *weight.value < 0 {
			return fmt.Errorf("health weight %s must be a non-negative integer, got %d", weight.name, *weight.value)
		}
	}
	return nil
}

func validateApplyOutputContract(opts *options) error {
	if !opts.Apply {
		return nil
	}
	if opts.Export != "" {
		return fmt.Errorf("--apply cannot be combined with --export because apply diagnostics and export data use different output contracts")
	}
	if opts.Format == "structured" || opts.ContextForAI || opts.Ask != "" {
		return fmt.Errorf("--apply cannot be combined with structured or AI-context output")
	}
	format, _ := selectOutputFromOptions(opts)
	switch normalizeOutputFormat(format) {
	case "", "table", "wide", "ja":
		return nil
	default:
		return fmt.Errorf("--apply requires human-readable table output; got --output=%q", opts.Output)
	}
}

func validateListOptions(opts *options) error {
	if opts.HealthScoreMin < -1 || opts.HealthScoreMin > 100 {
		return fmt.Errorf("--min-score must be in [0, 100] or -1 when unset, got %d", opts.HealthScoreMin)
	}
	maxScore := effectiveHealthScoreMax(opts)
	if maxScore < -1 || maxScore > 100 {
		return fmt.Errorf("--health-score must be in [0, 100] or -1 when unset, got %d", opts.HealthScoreMax)
	}
	if opts.HealthScoreMin >= 0 && maxScore >= 0 && opts.HealthScoreMin > maxScore {
		return fmt.Errorf("--min-score cannot be greater than --health-score")
	}
	if err := validateMode("--filter", normalizeSelector(opts.Filter), "", "all", "ok", "error", "limited", "scalinglimited", "issue"); err != nil {
		return err
	}
	if err := validateMode("--sort-by", normalizeSelector(opts.SortBy), "", "namespace", "name", "current", "currentreplicas", "desired", "desiredreplicas", "diff", "replicadiff", "difference", "age", "creationtimestamp", "health", "healthscore", "score", "problem", "issue", "min", "minreplicas", "max", "maxreplicas", "target"); err != nil {
		return err
	}
	return nil
}
