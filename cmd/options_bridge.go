package cmd

import "github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"

// EventOption is re-exported from internal/cmdoptions/events.go for use in
// cmd/ struct literals.
type EventOption = cmdoptions.EventOption

// This file is the single source of truth for the cmd -> internal/cmdoptions
// bridge. Every cmdoptions symbol referenced from cmd/ MUST go through one of
// the aliases, consts, or helpers below so there is exactly one vocabulary for
// presets and option types across all command files. Do not import
// internal/cmdoptions symbols (Preset*, CommandPresetOptions, ...) directly
// from individual command files; add or use the bridge entry here instead.

// Type aliases re-export the cmdoptions model under the names the cmd package
// uses in command wiring and struct literals.
type (
	options              = cmdoptions.Root
	commonOptions        = cmdoptions.Common
	statusOptions        = cmdoptions.Status
	listOptions          = cmdoptions.List
	watchOptions         = cmdoptions.Watch
	featuresOptions      = cmdoptions.Features
	outputTemplateConfig = cmdoptions.OutputTemplateConfig
	commandPresetOptions = cmdoptions.CommandPresetOptions
)

// Preset consts cover every CommandPreset defined in internal/cmdoptions. New
// presets must be added here so command files stay free of direct cmdoptions
// references.
const (
	presetDoctor           = cmdoptions.PresetDoctor
	presetExplain          = cmdoptions.PresetExplain
	presetMetricsProbe     = cmdoptions.PresetMetricsProbe
	presetReadiness        = cmdoptions.PresetReadiness
	presetBlockers         = cmdoptions.PresetBlockers
	presetBundle           = cmdoptions.PresetBundle
	presetIncidentBundle   = cmdoptions.PresetIncidentBundle
	presetSupportBundle    = cmdoptions.PresetSupportBundle
	presetCapacityPlan     = cmdoptions.PresetCapacityPlan
	presetCapacityGap      = cmdoptions.PresetCapacityGap
	presetPreflight        = cmdoptions.PresetPreflight
	presetRollout          = cmdoptions.PresetRollout
	presetContainerAdvisor = cmdoptions.PresetContainerAdvisor
	presetWhyNotScale      = cmdoptions.PresetWhyNotScale
	presetTrace            = cmdoptions.PresetTrace
	presetPath             = cmdoptions.PresetPath
	presetNodeContext      = cmdoptions.PresetNodeContext
	presetRolloutContext   = cmdoptions.PresetRolloutContext
	presetHistory          = cmdoptions.PresetHistory
	presetSLO              = cmdoptions.PresetSLO
)

func copyOptions(opts *options) options {
	return opts.Copy()
}

func applyCommandPreset(opts *options, preset cmdoptions.CommandPreset, extra ...commandPresetOptions) options {
	return cmdoptions.ApplyCommandPreset(*opts, preset, extra...)
}

// defaultRootOptions seeds an options struct with the shared defaults.
func defaultRootOptions() options {
	return cmdoptions.DefaultRoot()
}

// validAnalysisProfiles exposes the analysis-profile names for shell completion.
func validAnalysisProfiles() []string {
	return cmdoptions.ValidAnalysisProfiles()
}
