package cmdoptions

import (
	"fmt"
	"strings"
)

// AnalysisProfile names a bundled set of status enrichment flags for common
// operator workflows. Use --analysis-profile on status to enable a preset
// without spelling out many boolean flags.
type AnalysisProfile string

// AnalysisProfile values name the predefined enrichment profiles that bundle
// feature flags for a given workflow.
const (
	ProfileQuick     AnalysisProfile = "quick"
	ProfileStandard  AnalysisProfile = "standard"
	ProfileIncident  AnalysisProfile = "incident"
	ProfileDoctor    AnalysisProfile = "doctor"
	ProfileMetrics   AnalysisProfile = "metrics"
	ProfileCapacity  AnalysisProfile = "capacity"
	ProfileReadiness AnalysisProfile = "readiness"
	ProfileDeep      AnalysisProfile = "deep"
)

// ValidAnalysisProfiles returns supported --analysis-profile values.
func ValidAnalysisProfiles() []string {
	return []string{
		string(ProfileQuick),
		string(ProfileStandard),
		string(ProfileIncident),
		string(ProfileDoctor),
		string(ProfileMetrics),
		string(ProfileCapacity),
		string(ProfileReadiness),
		string(ProfileDeep),
	}
}

// Set implements pflag.Value for --analysis-profile.
func (p *AnalysisProfile) Set(raw string) error {
	parsed, err := ParseAnalysisProfile(raw)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

func (p AnalysisProfile) String() string { return string(p) }

// Type implements pflag.Value.
func (p AnalysisProfile) Type() string { return "analysisProfile" }

// ParseAnalysisProfile normalizes and validates a profile name.
func ParseAnalysisProfile(raw string) (AnalysisProfile, error) {
	normalized := AnalysisProfile(strings.ToLower(strings.TrimSpace(raw)))
	if normalized == "" {
		return "", nil
	}
	for _, valid := range ValidAnalysisProfiles() {
		if normalized == AnalysisProfile(valid) {
			return normalized, nil
		}
	}
	return "", fmt.Errorf("unknown analysis profile %q; supported: %s", raw, strings.Join(ValidAnalysisProfiles(), ", "))
}

// ApplyAnalysisProfile enables the feature bundle for a named profile.
// Profiles are additive: they only turn flags on, never off.
func ApplyAnalysisProfile(f *Features, profile AnalysisProfile) {
	switch profile {
	case ProfileQuick:
		f.Interpret = true
		f.Explain = true
	case ProfileStandard:
		f.Explain = true
	case ProfileIncident:
		applyDoctorFeatures(f)
		f.ScaleoutBlockers = true
		f.RolloutImpact = true
		f.ControllerProfile = true
	case ProfileDoctor:
		applyDoctorFeatures(f)
	case ProfileMetrics:
		applyMetricsProbeFeatures(f)
	case ProfileCapacity:
		applyCapacityGapFeatures(f)
	case ProfileReadiness:
		applyReadinessFeatures(f)
	case ProfileDeep:
		// The deep tier bundles the capacity/rollout/readiness/adapter
		// diagnostics that the one-flag --deep switch enables. It mirrors
		// normalizeCapacityFlags' --deep expansion so --analysis-profile deep
		// and --deep are equivalent entry points.
		f.Deep = true
		applyDeepFeatures(f)
	}
}

// applyDeepFeatures enables the deep-tier enricher group. Shared by the --deep
// flag (normalizeCapacityFlags) and --analysis-profile deep so the two entry
// points cannot drift.
func applyDeepFeatures(f *Features) {
	f.CapacityContext = true
	f.CapacityHeadroom = true
	f.CapacityDeep = true
	f.ScalePath = true
	f.RolloutImpact = true
	f.ReadinessImpact = true
	f.AdapterDiagnostics = true
	f.ExplainPods = true
}

func applyDoctorFeatures(f *Features) {
	f.Explain = true
	f.DiagnoseMetrics = true
	f.MetricsFreshness = true
	f.CheckResources = true
	f.ExplainPods = true
	f.CapacityContext = true
	f.GitOpsCheck = true
	f.MetricContract = true
	f.ChurnDetect = true
	f.MetricHints = true
	f.ContainerAdvisor = true
	f.BehaviorAdvisor = true
	f.CapacityDeep = true
	f.Rollout = true
	f.ReadinessImpact = true
	f.ScalePath = true
	f.FlappingAdvisor = true
	f.TrendAnomaly = true
	f.AdapterDiagnostics = true
}

func applyMetricsProbeFeatures(f *Features) {
	f.DiagnoseMetrics = true
	f.MetricsFreshness = true
	f.MetricContract = true
	f.AdapterDiagnostics = true
	f.MetricHints = true
}

func applyCapacityGapFeatures(f *Features) {
	f.Explain = true
	f.ExplainPods = true
	f.ReadinessImpact = true
	f.CapacityHeadroom = true
	f.CapacityDeep = true
	f.ScalePath = true
	f.ScaleoutBlockers = true
}

func applyReadinessFeatures(f *Features) {
	f.Explain = true
	f.ReadinessImpact = true
	f.ExplainPods = true
	f.ScalePath = true
	f.RolloutImpact = true
	f.MetricsFreshness = true
	f.ControllerProfile = true
}

// CommandPreset names the built-in enrichment bundle for a subcommand.
type CommandPreset string

// CommandPreset values select the enrichment bundle that ApplyCommandPreset
// applies to a copy of Root for a given subcommand.
const (
	PresetDoctor           CommandPreset = "doctor"
	PresetExplain          CommandPreset = "explain"
	PresetMetricsProbe     CommandPreset = "metrics-probe"
	PresetReadiness        CommandPreset = "readiness"
	PresetBlockers         CommandPreset = "blockers"
	PresetBundle           CommandPreset = "bundle"
	PresetIncidentBundle   CommandPreset = "incident-bundle"
	PresetSupportBundle    CommandPreset = "support-bundle"
	PresetCapacityPlan     CommandPreset = "capacity-plan"
	PresetCapacityGap      CommandPreset = "capacity-gap"
	PresetPreflight        CommandPreset = "preflight"
	PresetRollout          CommandPreset = "rollout"
	PresetContainerAdvisor CommandPreset = "container-advisor"
	PresetWhyNotScale      CommandPreset = "why-not-scale"
	PresetTrace            CommandPreset = "trace"
	PresetPath             CommandPreset = "path"
	PresetNodeContext      CommandPreset = "node-context"
	PresetRolloutContext   CommandPreset = "rollout-context"
	PresetHistory          CommandPreset = "history"
	PresetSLO              CommandPreset = "slo"
)

// CommandPresetOptions holds non-feature overrides applied with a command preset.
type CommandPresetOptions struct {
	DecisionTraceFormat string
	StructuredFormat    bool
	Events              *EventOption
	KEDA                string
	VPA                 string
}

// presetAppliers maps each command preset to the function that mutates a local
// Root copy with that preset's enrichment bundle. Splitting the per-preset
// logic into named functions keeps ApplyCommandPreset a flat dispatch and
// holds cyclomatic complexity below the gocyclo threshold.
var presetAppliers = map[CommandPreset]func(*Root, CommandPresetOptions){
	PresetDoctor:           applyPresetDoctor,
	PresetExplain:          applyPresetExplain,
	PresetMetricsProbe:     applyPresetMetricsProbe,
	PresetReadiness:        applyPresetReadiness,
	PresetBlockers:         applyPresetBlockers,
	PresetBundle:           applyPresetBundle,
	PresetIncidentBundle:   applyPresetIncidentBundle,
	PresetSupportBundle:    applyPresetSupportBundle,
	PresetCapacityPlan:     applyPresetCapacityPlan,
	PresetCapacityGap:      applyPresetCapacityGap,
	PresetPreflight:        applyPresetPreflight,
	PresetRollout:          applyPresetRollout,
	PresetContainerAdvisor: applyPresetContainerAdvisor,
	PresetWhyNotScale:      applyPresetWhyNotScale,
	PresetTrace:            applyPresetTrace,
	PresetPath:             applyPresetPath,
	PresetNodeContext:      applyPresetNodeContext,
	PresetRolloutContext:   applyPresetRolloutContext,
	PresetHistory:          applyPresetHistory,
	PresetSLO:              applyPresetSLO,
}

// ApplyCommandPreset returns a shallow copy of root with the subcommand's
// enrichment bundle applied. Use this instead of mutating the shared opts.
func ApplyCommandPreset(root Root, preset CommandPreset, extra ...CommandPresetOptions) Root {
	local := root.Copy()
	var opts CommandPresetOptions
	if len(extra) > 0 {
		opts = extra[0]
	}
	if apply, ok := presetAppliers[preset]; ok {
		apply(&local, opts)
	}
	return local
}

func applyPresetDoctor(local *Root, _ CommandPresetOptions) {
	applyDoctorFeatures(&local.Features)
}

func applyPresetExplain(local *Root, opts CommandPresetOptions) {
	local.Explain = true
	local.DecisionTrace = true
	if opts.DecisionTraceFormat != "" {
		local.DecisionTraceFormat = opts.DecisionTraceFormat
	} else {
		local.DecisionTraceFormat = "json"
	}
	if opts.StructuredFormat && local.Output == "" {
		local.Format = "structured"
	}
}

func applyPresetMetricsProbe(local *Root, _ CommandPresetOptions) {
	applyMetricsProbeFeatures(&local.Features)
}

func applyPresetReadiness(local *Root, _ CommandPresetOptions) {
	applyReadinessFeatures(&local.Features)
}

func applyPresetBlockers(local *Root, opts CommandPresetOptions) {
	local.CapacityContext = true
	local.ExplainPods = true
	if opts.Events != nil {
		local.Events = *opts.Events
	} else {
		local.Events = EventOption{Enabled: true, Limit: 10}
	}
}

func applyPresetBundle(local *Root, opts CommandPresetOptions) {
	applyDoctorFeatures(&local.Features)
	local.ReadinessImpact = true
	local.RolloutImpact = true
	local.ScaleoutBlockers = true
	local.ControllerProfile = true
	local.ScalePath = true
	if opts.KEDA != "" {
		local.KEDA = opts.KEDA
	} else {
		local.KEDA = "on"
	}
	if opts.VPA != "" {
		local.VPA = opts.VPA
	} else {
		local.VPA = "on"
	}
	if opts.Events != nil {
		local.Events = *opts.Events
	} else {
		local.Events = EventOption{Enabled: true, Limit: 20}
	}
}

func applyPresetIncidentBundle(local *Root, _ CommandPresetOptions) {
	local.ReadinessImpact = true
	local.RolloutImpact = true
	local.ScaleoutBlockers = true
	local.ControllerProfile = true
}

func applyPresetSupportBundle(local *Root, _ CommandPresetOptions) {
	applyDoctorFeatures(&local.Features)
	local.ReadinessImpact = true
	local.RolloutImpact = true
	local.ScaleoutBlockers = true
	local.ControllerProfile = true
	local.KEDA = "on"
	local.VPA = "on"
}

func applyPresetCapacityPlan(local *Root, _ CommandPresetOptions) {
	local.CheckResources = true
	local.CapacityContext = true
	local.CapacityDeep = true
	local.ExplainPods = true
}

func applyPresetCapacityGap(local *Root, _ CommandPresetOptions) {
	applyCapacityGapFeatures(&local.Features)
}

func applyPresetPreflight(local *Root, _ CommandPresetOptions) {
	local.CheckResources = true
	local.CapacityContext = true
	local.CapacityDeep = true
	local.ExplainPods = true
}

func applyPresetRollout(local *Root, _ CommandPresetOptions) {
	local.Rollout = true
	local.RolloutImpact = true
	local.ReadinessImpact = true
	local.ExplainPods = true
}

func applyPresetContainerAdvisor(local *Root, _ CommandPresetOptions) {
	local.Explain = true
	local.ExplainPods = true
	local.CheckResources = true
	local.ContainerAdvisor = true
}

func applyPresetWhyNotScale(local *Root, _ CommandPresetOptions) {
	local.Explain = true
	local.DiagnoseMetrics = true
	local.MetricsFreshness = true
	local.ReadinessImpact = true
	local.ScalePath = true
	local.CapacityHeadroom = true
}

func applyPresetTrace(local *Root, _ CommandPresetOptions) {
	local.DecisionTrace = true
}

func applyPresetPath(local *Root, _ CommandPresetOptions) {
	local.ScalePath = true
}

func applyPresetNodeContext(local *Root, _ CommandPresetOptions) {
	local.Explain = true
	local.ExplainPods = true
	local.CapacityContext = true
	local.CapacityHeadroom = true
	local.CapacityDeep = true
	local.ScalePath = true
	local.ScaleoutBlockers = true
	local.NodeAutoscaler = true
	local.Karpenter = true
}

func applyPresetRolloutContext(local *Root, _ CommandPresetOptions) {
	local.Explain = true
	local.ExplainPods = true
	local.ReadinessImpact = true
	local.Rollout = true
	local.RolloutImpact = true
	local.ScalePath = true
}

func applyPresetHistory(local *Root, _ CommandPresetOptions) {
	local.Trend = true
	local.ChurnDetect = true
}

func applyPresetSLO(local *Root, _ CommandPresetOptions) {
	local.Explain = true
	local.MetricHints = true
}
