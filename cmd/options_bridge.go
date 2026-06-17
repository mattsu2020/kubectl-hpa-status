package cmd

import "github.com/mattsui2020/kubectl-hpa-status/internal/cmdoptions"

type (
	options              = cmdoptions.Root
	commonOptions        = cmdoptions.Common
	featureFlags         = cmdoptions.Features
	statusOptions        = cmdoptions.Status
	listOptions          = cmdoptions.List
	watchOptions         = cmdoptions.Watch
	EventOption          = cmdoptions.EventOption
	outputTemplateConfig = cmdoptions.OutputTemplateConfig
	analysisProfile      = cmdoptions.AnalysisProfile
	commandPreset        = cmdoptions.CommandPreset
)

const (
	presetDoctor         = cmdoptions.PresetDoctor
	presetExplain        = cmdoptions.PresetExplain
	presetMetricsProbe   = cmdoptions.PresetMetricsProbe
	presetReadiness      = cmdoptions.PresetReadiness
	presetBlockers       = cmdoptions.PresetBlockers
	presetBundle         = cmdoptions.PresetBundle
	presetIncidentBundle = cmdoptions.PresetIncidentBundle
	presetSupportBundle  = cmdoptions.PresetSupportBundle
	presetCapacityPlan   = cmdoptions.PresetCapacityPlan
	presetCapacityGap    = cmdoptions.PresetCapacityGap
	presetPreflight      = cmdoptions.PresetPreflight
	presetRollout        = cmdoptions.PresetRollout
	presetContainerAdvisor = cmdoptions.PresetContainerAdvisor
	presetWhyNotScale    = cmdoptions.PresetWhyNotScale
	presetTrace          = cmdoptions.PresetTrace
	presetPath           = cmdoptions.PresetPath
	presetNodeContext    = cmdoptions.PresetNodeContext
	presetRolloutContext = cmdoptions.PresetRolloutContext
	presetHistory        = cmdoptions.PresetHistory
	presetSLO            = cmdoptions.PresetSLO
)

func copyOptions(opts *options) options {
	return opts.Copy()
}

func applyCommandPreset(opts *options, preset commandPreset, extra ...cmdoptions.CommandPresetOptions) options {
	return cmdoptions.ApplyCommandPreset(*opts, preset, extra...)
}