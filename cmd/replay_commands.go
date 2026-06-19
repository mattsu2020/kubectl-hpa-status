package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// This file holds the `record` and `replay` cobra command constructors plus
// their thin dispatch helpers. They were split out of timeline.go so the live
// timeline command and the recording/replay surface can evolve independently;
// the heavy lifting still lives in replay_lab*.go and runRecord (timeline.go).

func newRecordCommand(opts *options) *cobra.Command {
	var duration time.Duration
	var interval time.Duration
	var outputPath string

	cmd := &cobra.Command{
		Use:               "record [NAME]",
		Short:             "Record durable HPA decision snapshots to JSONL",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := outputPath
			if path == "" && opts.Output != "" && !isKnownOutputFormat(opts.Output) {
				path = opts.Output
			}
			if path == "" {
				path = "hpa-history.jsonl"
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			if duration > 0 {
				var cancel context.CancelFunc
				ctx, cancel := context.WithTimeout(cmd.Context(), duration)
				defer cancel()
				return runRecord(ctx, cmd.OutOrStdout(), opts, name, interval, path)
			}
			return runRecord(cmd.Context(), cmd.OutOrStdout(), opts, name, interval, path)
		},
	}
	cmd.Flags().DurationVar(&duration, "duration", 15*time.Minute, "total recording duration")
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Second, "polling interval")
	cmd.Flags().StringVar(&outputPath, "output-file", "", "path to durable JSONL history file; -o FILE is also accepted for record")
	return cmd
}

func newReplayCommand(opts *options) *cobra.Command {
	var fromRecord string
	var candidates []string
	var compare string
	var score string
	var setOverrides []string
	var replayHPA string
	var propose string
	var setMaxReplicas int32
	var setMinReplicas int32
	var setScaleDownStabilization time.Duration
	var setCPUTarget int32
	var setMemoryTarget int32
	cmd := &cobra.Command{
		Use:   "replay [FILE|NAME]",
		Short: "Replay a recorded HPA timeline trace or run a what-if lab from record",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// --propose is an alias for --candidate.
			if propose != "" && len(candidates) == 0 {
				candidates = append([]string{propose}, candidates...)
			}
			if replayHPA != "" {
				return runReplayWithHPA(cmd.OutOrStdout(), opts, replayHPA, args, candidates, setOverrides, setMaxReplicas, setMinReplicas, setScaleDownStabilization, setCPUTarget, setMemoryTarget, compare, score)
			}
			if fromRecord != "" {
				return runReplayWithFromRecord(cmd.OutOrStdout(), opts, fromRecord, args, candidates, setOverrides, compare, score)
			}
			if len(candidates) > 0 || score != "" {
				return runReplayWithCandidateOrScore(cmd.OutOrStdout(), opts, replayHPA, args, candidates, setOverrides, setMaxReplicas, setMinReplicas, setScaleDownStabilization, setCPUTarget, setMemoryTarget, score)
			}
			if len(args) != 1 {
				return fmt.Errorf("replay requires FILE, or NAME with --from-record")
			}
			return runReplay(cmd.OutOrStdout(), opts, args[0])
		},
	}
	cmd.Flags().StringVar(&fromRecord, "from-record", "", "read durable JSONL/JSON trace written by record")
	cmd.Flags().StringArrayVar(&candidates, "candidate", nil, "candidate HPA YAML to compare against recorded behavior; repeatable")
	cmd.Flags().StringVar(&propose, "propose", "", "proposed behavior YAML file (alias for --candidate)")
	cmd.Flags().StringVar(&compare, "compare", "current,candidate", "comparison mode for --from-record: current,candidate")
	cmd.Flags().StringVar(&score, "score", "", "comma-separated replay scoring dimensions to emphasize, e.g. slo,cost,churn")
	cmd.Flags().StringArrayVar(&setOverrides, "set", nil, "candidate override for replay lab, e.g. maxReplicas=30 or scaleDown.stabilizationWindowSeconds=600")
	cmd.Flags().StringVar(&replayHPA, "hpa", "", "HPA name when FILE is passed as the replay input")
	cmd.Flags().Int32Var(&setMaxReplicas, "set-max-replicas", 0, "candidate maxReplicas for replay lab")
	cmd.Flags().Int32Var(&setMinReplicas, "set-min-replicas", 0, "candidate minReplicas for replay lab")
	cmd.Flags().DurationVar(&setScaleDownStabilization, "set-scale-down-stabilization", 0, "candidate scaleDown.stabilizationWindowSeconds for replay lab")
	cmd.Flags().Int32Var(&setCPUTarget, "set-cpu-target", 0, "candidate CPU averageUtilization target percentage (reported as an estimated limitation when raw metrics are unavailable)")
	cmd.Flags().Int32Var(&setMemoryTarget, "set-memory-target", 0, "candidate memory averageUtilization target percentage (reported as an estimated limitation when raw metrics are unavailable)")
	return cmd
}

// runReplayWithHPA handles the `replay --hpa NAME FILE` form.
func runReplayWithHPA(out io.Writer, opts *options, replayHPA string, args []string, candidates, setOverrides []string, setMaxReplicas, setMinReplicas int32, setScaleDownStabilization time.Duration, setCPUTarget, setMemoryTarget int32, compare, score string) error {
	if len(args) != 1 {
		return fmt.Errorf("replay --hpa requires a record FILE argument")
	}
	if compare != "" && compare != "current,candidate" {
		return fmt.Errorf("unsupported --compare %q (use current,candidate)", compare)
	}
	overrides, err := parseSimulateOverrides(setOverrides)
	if err != nil {
		return err
	}
	addReplayShortcutOverrides(overrides, setMaxReplicas, setMinReplicas, setScaleDownStabilization, setCPUTarget, setMemoryTarget)
	return runReplayPolicyLab(out, opts, replayHPA, args[0], candidates, overrides, score)
}

// runReplayWithFromRecord handles the `replay --from-record FILE NAME` form.
func runReplayWithFromRecord(out io.Writer, opts *options, fromRecord string, args []string, candidates, setOverrides []string, compare, score string) error {
	if len(args) != 1 {
		return fmt.Errorf("replay --from-record requires an HPA name")
	}
	if compare != "" && compare != "current,candidate" {
		return fmt.Errorf("unsupported --compare %q (use current,candidate)", compare)
	}
	overrides, err := parseSimulateOverrides(setOverrides)
	if err != nil {
		return err
	}
	return runReplayPolicyLab(out, opts, args[0], fromRecord, candidates, overrides, score)
}

// runReplayWithCandidateOrScore handles the `replay --candidate/--score FILE` form.
func runReplayWithCandidateOrScore(out io.Writer, opts *options, replayHPA string, args []string, candidates, setOverrides []string, setMaxReplicas, setMinReplicas int32, setScaleDownStabilization time.Duration, setCPUTarget, setMemoryTarget int32, score string) error {
	if len(args) != 1 {
		return fmt.Errorf("replay with --candidate or --score requires a record FILE argument")
	}
	overrides, err := parseSimulateOverrides(setOverrides)
	if err != nil {
		return err
	}
	addReplayShortcutOverrides(overrides, setMaxReplicas, setMinReplicas, setScaleDownStabilization, setCPUTarget, setMemoryTarget)
	return runReplayPolicyLab(out, opts, replayHPA, args[0], candidates, overrides, score)
}

func addReplayShortcutOverrides(overrides map[string]string, maxReplicas, minReplicas int32, stabilization time.Duration, cpuTarget, memoryTarget int32) {
	if maxReplicas > 0 {
		overrides["maxReplicas"] = fmt.Sprint(maxReplicas)
	}
	if minReplicas > 0 {
		overrides["minReplicas"] = fmt.Sprint(minReplicas)
	}
	if stabilization > 0 {
		overrides["scaleDown.stabilizationWindowSeconds"] = fmt.Sprint(int(stabilization.Seconds()))
	}
	if cpuTarget > 0 {
		overrides["cpu.targetAverageUtilization"] = fmt.Sprint(cpuTarget)
	}
	if memoryTarget > 0 {
		overrides["memory.targetAverageUtilization"] = fmt.Sprint(memoryTarget)
	}
}
