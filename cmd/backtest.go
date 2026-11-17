package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/yaml"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/backtest"
)

type backtestFlags struct {
	fromRecord    string
	candidates    []string
	minReplicas   int32
	maxReplicas   int32
	targetCPU     int32
	scaleUpStab   int32
	scaleDownStab int32
	prescales     []string
	prom          prometheusImportFlags
}

func newBacktestCommand(opts *options) *cobra.Command {
	flags := backtestFlags{}
	cmd := &cobra.Command{
		Use:   "backtest [NAME]",
		Short: "Replay recorded or Prometheus history against candidate HPA configurations",
		Long:  "Backtest candidate HPA configurations (min/max replicas, target utilization, stabilization windows, and pre-scale schedules) against recorded JSONL or Prometheus-imported HPA history and rank the candidates.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runBacktest(cmd.Context(), cmd.OutOrStdout(), opts, name, flags)
		},
	}
	cmd.Flags().StringVar(&flags.fromRecord, "from-record", "", fromRecordFlagDescription)
	cmd.Flags().StringSliceVar(&flags.candidates, "candidate", nil, "HPA YAML/JSON manifest file defining a candidate configuration (repeatable)")
	cmd.Flags().Int32Var(&flags.minReplicas, "min", 0, "inline candidate minReplicas override (0 uses default)")
	cmd.Flags().Int32Var(&flags.maxReplicas, "max", 0, "inline candidate maxReplicas override")
	cmd.Flags().Int32Var(&flags.targetCPU, "target-cpu", 0, "inline candidate CPU utilization target percentage")
	cmd.Flags().Int32Var(&flags.scaleUpStab, "scale-up-stabilization", 0, "inline candidate scaleUp stabilizationWindowSeconds")
	cmd.Flags().Int32Var(&flags.scaleDownStab, "scale-down-stabilization", 0, "inline candidate scaleDown stabilizationWindowSeconds")
	cmd.Flags().StringSliceVar(&flags.prescales, "prescale", nil, "recurring pre-scale window as HH:MM-HH:MM=REPLICAS (repeatable)")
	addPrometheusImportFlags(cmd, &flags.prom, true)
	return cmd
}

func runBacktest(ctx context.Context, out io.Writer, opts *options, name string, flags backtestFlags) error {
	trace, source, err := loadBacktestTrace(ctx, opts, name, flags)
	if err != nil {
		return err
	}

	candidates, err := buildBacktestCandidates(*trace, flags)
	if err != nil {
		return err
	}

	report, err := backtest.Run(backtest.Input{
		Trace:      *trace,
		Source:     source,
		Candidates: candidates,
	})
	if err != nil {
		return err
	}

	format, _ := selectOutputFromOptions(opts)
	if format == "markdown" || format == "md" {
		return backtest.WriteMarkdown(out, report)
	}
	return renderWithOutput(out, opts, report, func(w io.Writer) error {
		return backtest.WriteText(w, report)
	})
}

func loadBacktestTrace(ctx context.Context, opts *options, name string, flags backtestFlags) (*hpaanalysis.TimelineTrace, string, error) {
	if flags.fromRecord != "" && flags.prom.requested() {
		return nil, "", fmt.Errorf("--from-record and --from-prometheus cannot be used together")
	}
	if flags.prom.requested() {
		if strings.TrimSpace(name) == "" {
			return nil, "", fmt.Errorf("HPA NAME argument is required when using --from-prometheus")
		}
		imported, err := loadTraceFromPrometheus(ctx, opts, name, flags.prom.Since, flags.prom)
		if err != nil {
			return nil, "", err
		}
		return &imported.Trace, imported.Provenance, nil
	}
	if flags.fromRecord == "" {
		return nil, "", fmt.Errorf("either --from-record or --from-prometheus is required")
	}
	if strings.TrimSpace(name) == "" {
		inferred, err := inferRecordedTraceName(flags.fromRecord, opts.Namespace)
		if err != nil {
			return nil, "", err
		}
		name = inferred
	}
	trace, err := loadRecordedTrace(flags.fromRecord, opts.Namespace, name)
	if err != nil {
		return nil, "", err
	}
	return trace, flags.fromRecord, nil
}

func buildBacktestCandidates(trace hpaanalysis.TimelineTrace, flags backtestFlags) ([]backtest.CandidateSpec, error) {
	defaultMax := inferTraceMaxReplicas(trace)
	var specs []backtest.CandidateSpec

	for i, path := range flags.candidates {
		spec, err := loadBacktestCandidateFile(path, i, defaultMax)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}

	if hasInlineBacktestOverrides(flags) {
		inline, err := buildInlineBacktestCandidate(flags, defaultMax)
		if err != nil {
			return nil, err
		}
		specs = append(specs, inline)
	}

	if len(specs) == 0 {
		specs = defaultBacktestPresets(defaultMax)
	}
	return specs, nil
}

func hasInlineBacktestOverrides(flags backtestFlags) bool {
	return flags.minReplicas > 0 ||
		flags.maxReplicas > 0 ||
		flags.targetCPU > 0 ||
		flags.scaleUpStab > 0 ||
		flags.scaleDownStab > 0 ||
		len(flags.prescales) > 0
}

func buildInlineBacktestCandidate(flags backtestFlags, defaultMax int32) (backtest.CandidateSpec, error) {
	maxRep := defaultMax
	if flags.maxReplicas > 0 {
		maxRep = flags.maxReplicas
	}
	spec := backtest.CandidateSpec{
		Name:                          "inline-candidate",
		MaxReplicas:                   maxRep,
		ScaleUpStabilizationSeconds:   flags.scaleUpStab,
		ScaleDownStabilizationSeconds: flags.scaleDownStab,
	}
	if flags.minReplicas > 0 {
		minRep := flags.minReplicas
		spec.MinReplicas = &minRep
	}
	if flags.targetCPU > 0 {
		target := flags.targetCPU
		spec.TargetUtilization = &target
	}
	for _, raw := range flags.prescales {
		win, err := parsePrescaleWindow(raw)
		if err != nil {
			return backtest.CandidateSpec{}, err
		}
		spec.PrescaleWindows = append(spec.PrescaleWindows, win)
	}
	return spec, nil
}

func loadBacktestCandidateFile(path string, idx int, defaultMax int32) (backtest.CandidateSpec, error) {
	data, err := readFileBounded(path)
	if err != nil {
		return backtest.CandidateSpec{}, fmt.Errorf("read candidate %s: %w", path, err)
	}
	var hpa autoscalingv2.HorizontalPodAutoscaler
	if err := yaml.Unmarshal(data, &hpa); err != nil {
		return backtest.CandidateSpec{}, fmt.Errorf("parse candidate %s: %w", path, err)
	}
	maxRep := hpa.Spec.MaxReplicas
	if maxRep <= 0 {
		maxRep = defaultMax
	}
	spec := backtest.CandidateSpec{
		Name:        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		MinReplicas: hpa.Spec.MinReplicas,
		MaxReplicas: maxRep,
	}
	if spec.Name == "" {
		spec.Name = fmt.Sprintf("candidate-%d", idx+1)
	}
	if hpa.Spec.Behavior != nil {
		if hpa.Spec.Behavior.ScaleUp != nil {
			spec.ScaleUpRules = hpa.Spec.Behavior.ScaleUp
			if hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil {
				spec.ScaleUpStabilizationSeconds = *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
			}
		}
		if hpa.Spec.Behavior.ScaleDown != nil {
			spec.ScaleDownRules = hpa.Spec.Behavior.ScaleDown
			if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
				spec.ScaleDownStabilizationSeconds = *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
			}
		}
	}
	for _, m := range hpa.Spec.Metrics {
		if m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource != nil && m.Resource.Name == "cpu" && m.Resource.Target.AverageUtilization != nil {
			util := *m.Resource.Target.AverageUtilization
			spec.TargetUtilization = &util
			break
		}
	}
	return spec, nil
}

func defaultBacktestPresets(defaultMax int32) []backtest.CandidateSpec {
	headroomMax := max(defaultMax, defaultMax+max(1, defaultMax/2))
	return []backtest.CandidateSpec{
		{
			Name:                          "baseline",
			MaxReplicas:                   defaultMax,
			ScaleDownStabilizationSeconds: 0,
		},
		{
			Name:                          "stabilized-5m",
			MaxReplicas:                   defaultMax,
			ScaleDownStabilizationSeconds: 300,
		},
		{
			Name:                          "stabilized-10m",
			MaxReplicas:                   defaultMax,
			ScaleDownStabilizationSeconds: 600,
		},
		{
			Name:                          "headroom-5m",
			MaxReplicas:                   headroomMax,
			ScaleDownStabilizationSeconds: 300,
		},
	}
}

func inferTraceMaxReplicas(trace hpaanalysis.TimelineTrace) int32 {
	var peak int32 = 10
	for _, snap := range trace.Snapshots {
		if snap.Desired > peak {
			peak = snap.Desired
		}
		if snap.Current > peak {
			peak = snap.Current
		}
	}
	return peak
}

// parsePrescaleWindow parses "HH:MM-HH:MM=REPLICAS".
func parsePrescaleWindow(raw string) (backtest.PrescaleWindow, error) {
	rangePart, repPart, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if !ok {
		return backtest.PrescaleWindow{}, fmt.Errorf("invalid --prescale %q (expected HH:MM-HH:MM=REPLICAS)", raw)
	}
	startStr, endStr, ok := strings.Cut(rangePart, "-")
	if !ok {
		return backtest.PrescaleWindow{}, fmt.Errorf("invalid --prescale time range %q (expected HH:MM-HH:MM)", rangePart)
	}
	startMin, err := parseClockMinute(startStr)
	if err != nil {
		return backtest.PrescaleWindow{}, fmt.Errorf("invalid --prescale start %q: %w", startStr, err)
	}
	endMin, err := parseClockMinute(endStr)
	if err != nil {
		return backtest.PrescaleWindow{}, fmt.Errorf("invalid --prescale end %q: %w", endStr, err)
	}
	rep, err := strconv.ParseInt(strings.TrimSpace(repPart), 10, 32)
	if err != nil || rep < 1 {
		return backtest.PrescaleWindow{}, fmt.Errorf("invalid --prescale replicas %q", repPart)
	}
	return backtest.PrescaleWindow{
		StartMinute: startMin,
		EndMinute:   endMin,
		MinReplicas: int32(rep),
	}, nil
}

func parseClockMinute(s string) (int, error) {
	hhStr, mmStr, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("expected HH:MM")
	}
	hh, err := strconv.Atoi(hhStr)
	if err != nil || hh < 0 || hh > 23 {
		return 0, fmt.Errorf("hour must be 0..23")
	}
	mm, err := strconv.Atoi(mmStr)
	if err != nil || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("minute must be 0..59")
	}
	return hh*60 + mm, nil
}
