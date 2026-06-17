package cmd

import "github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"

// EventOption is re-exported from cmdoptions for use in cmd/ struct literals.
type EventOption = cmdoptions.EventOption

// Package-level type aliases re-export the cmdoptions model under the names
// the cmd package historically used, so existing command files compile without
// rewriting every reference.
type (
	options              = cmdoptions.Root
	outputTemplateConfig = cmdoptions.OutputTemplateConfig
	commandPreset        = cmdoptions.CommandPreset
)

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

func applyCommandPreset(opts *options, preset commandPreset, extra ...cmdoptions.CommandPresetOptions) options {
	return cmdoptions.ApplyCommandPreset(*opts, preset, extra...)
}
