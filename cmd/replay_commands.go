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
	cmd.Flags().DurationVar(&interval, "interval", defaultPollInterval, "polling interval")
	cmd.Flags().StringVar(&outputPath, "output-file", "", "path to durable JSONL history file; -o FILE is also accepted for record")
	return cmd
}

func newReplayCommand(opts *options) *cobra.Command {
	request := ReplayRequest{}
	cmd := &cobra.Command{
		Use:   "replay [FILE|NAME]",
		Short: "Replay a recorded HPA timeline trace or run a what-if lab from record",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// --propose is an alias for --candidate.
			if request.Propose != "" && len(request.Candidates) == 0 {
				request.Candidates = append([]string{request.Propose}, request.Candidates...)
			}
			if request.HPA != "" {
				return runReplayWithHPA(cmd.OutOrStdout(), opts, request, args)
			}
			if request.FromRecord != "" {
				return runReplayWithFromRecord(cmd.OutOrStdout(), opts, request, args)
			}
			if len(request.Candidates) > 0 || request.Score != "" {
				return runReplayWithCandidateOrScore(cmd.OutOrStdout(), opts, request, args)
			}
			if len(args) != 1 {
				return fmt.Errorf("replay requires FILE, or NAME with --from-record")
			}
			return runReplay(cmd.OutOrStdout(), opts, args[0])
		},
	}
	cmd.Flags().StringVar(&request.FromRecord, "from-record", "", "read durable JSONL/JSON trace written by record")
	cmd.Flags().StringArrayVar(&request.Candidates, "candidate", nil, "candidate HPA YAML to compare against recorded behavior; repeatable")
	cmd.Flags().StringVar(&request.Propose, "propose", "", "proposed behavior YAML file (alias for --candidate)")
	cmd.Flags().StringVar(&request.Compare, "compare", "current,candidate", "comparison mode for --from-record: current,candidate")
	cmd.Flags().StringVar(&request.Score, "score", "", "comma-separated replay scoring dimensions to emphasize, e.g. slo,cost,churn")
	cmd.Flags().StringArrayVar(&request.SetOverrides, "set", nil, "candidate override for replay lab, e.g. maxReplicas=30 or scaleDown.stabilizationWindowSeconds=600")
	cmd.Flags().StringVar(&request.HPA, "hpa", "", "HPA name when FILE is passed as the replay input")
	cmd.Flags().Int32Var(&request.MaxReplicas, "set-max-replicas", 0, "candidate maxReplicas for replay lab")
	cmd.Flags().Int32Var(&request.MinReplicas, "set-min-replicas", 0, "candidate minReplicas for replay lab")
	cmd.Flags().DurationVar(&request.ScaleDownStabilization, "set-scale-down-stabilization", 0, "candidate scaleDown.stabilizationWindowSeconds for replay lab")
	cmd.Flags().Int32Var(&request.CPUTarget, "set-cpu-target", 0, "candidate CPU averageUtilization target percentage (reported as an estimated limitation when raw metrics are unavailable)")
	cmd.Flags().Int32Var(&request.MemoryTarget, "set-memory-target", 0, "candidate memory averageUtilization target percentage (reported as an estimated limitation when raw metrics are unavailable)")
	return cmd
}

// ReplayRequest contains all inputs needed to replay a recorded HPA timeline.
type ReplayRequest struct {
	FromRecord, Compare, Score, HPA, Propose          string
	Candidates, SetOverrides                          []string
	MaxReplicas, MinReplicas, CPUTarget, MemoryTarget int32
	ScaleDownStabilization                            time.Duration
}

// runReplayWithHPA handles the `replay --hpa NAME FILE` form.
func runReplayWithHPA(out io.Writer, opts *options, request ReplayRequest, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("replay --hpa requires a record FILE argument")
	}
	return dispatchReplayPolicyLab(out, opts, request, request.HPA, args[0])
}

// runReplayWithFromRecord handles the `replay --from-record FILE NAME` form.
func runReplayWithFromRecord(out io.Writer, opts *options, request ReplayRequest, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("replay --from-record requires an HPA name")
	}
	return dispatchReplayPolicyLab(out, opts, request, args[0], request.FromRecord)
}

// runReplayWithCandidateOrScore handles the `replay --candidate/--score FILE` form.
func runReplayWithCandidateOrScore(out io.Writer, opts *options, request ReplayRequest, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("replay with --candidate or --score requires a record FILE argument")
	}
	return dispatchReplayPolicyLab(out, opts, request, request.HPA, args[0])
}

// dispatchReplayPolicyLab resolves the (name, recordPath) pair for one replay
// command form and runs the shared policy-lab pipeline. Every form shares the
// --compare validation, --set parsing, and the --set-max-replicas family of
// shortcut overrides so no path silently drops an override.
func dispatchReplayPolicyLab(out io.Writer, opts *options, request ReplayRequest, name, recordPath string) error {
	if request.Compare != "" && request.Compare != "current,candidate" {
		return fmt.Errorf("unsupported --compare %q (use current,candidate)", request.Compare)
	}
	overrides, err := parseSimulateOverrides(request.SetOverrides)
	if err != nil {
		return err
	}
	addReplayShortcutOverrides(overrides, request.MaxReplicas, request.MinReplicas, request.ScaleDownStabilization, request.CPUTarget, request.MemoryTarget)
	return runReplayPolicyLab(out, opts, name, recordPath, request.Candidates, overrides, request.Score)
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
