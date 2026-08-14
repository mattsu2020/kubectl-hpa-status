// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultPollInterval is the single CLI-side default for the polling `--interval`
// flag shared by the watch set (via the cmdoptions model) and the standalone
// timeline/replay commands. The options model carries its own 5s default in
// internal/cmdoptions; this constant keeps the per-command registrations from
// drifting if that canonical value ever changes.
const defaultPollInterval = 5 * time.Second

// registerCommonFlags registers the flags that genuinely apply to every
// command: cluster connection, output shaping, and client request tuning.
//
// Flags that only some commands act on (the mutation workflow, enrichment
// toggles, reporting, and watching) deliberately do NOT live here. They are
// built by newWorkflowFlagSet/newWatchFlagSet and attached per command, so a
// leaf utility such as `version` or `completion` rejects `--policy-guard-mode`
// instead of accepting and silently ignoring it.
func registerCommonFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().StringVarP(&opts.Namespace, "namespace", "n", "", "namespace")
	cmd.PersistentFlags().BoolVarP(&opts.AllNamespaces, "all-namespaces", "A", false, "list HPAs across all namespaces")
	cmd.PersistentFlags().StringVar(&opts.ContextName, "context", "", "kubeconfig context")
	cmd.PersistentFlags().StringVar(&opts.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
	cmd.PersistentFlags().StringVar(&opts.Cluster, "cluster", "", "kubeconfig cluster")
	cmd.PersistentFlags().StringVarP(&opts.Output, "output", "o", "", "output format: "+strings.Join(outputFlagDisplayValues, ", ")+", jsonpath=..., template=...")
	cmd.PersistentFlags().StringVar(&opts.OutputSchema, "output-schema", opts.OutputSchema, "structured status output schema: v1 or v2")
	cmd.PersistentFlags().StringVar(&opts.Template, "template", "", "template string to use when -o jsonpath or -o go-template/template is specified")
	cmd.PersistentFlags().BoolVar(&opts.Wide, "wide", false, "show additional columns in table output")
	cmd.PersistentFlags().StringVarP(&opts.Selector, "selector", "l", "", "label selector for list and scan, for example app=web,tier!=canary")
	cmd.PersistentFlags().StringVar(&opts.Color, "color", opts.Color, "colorize table output: "+strings.Join(validColorValues, ", "))
	cmd.PersistentFlags().StringVar(&opts.Lang, "lang", "", "language for table/section labels and the status summary line; analysis detail text remains English: "+strings.Join(validLangValues, ", "))
	cmd.PersistentFlags().BoolVarP(&opts.Debug, "debug", "v", false, "include internal analysis details such as ratios and health scoring inputs")
	cmd.PersistentFlags().StringVar(&opts.Config, "config", "", "optional config file for analysis settings such as health score weights")
	cmd.PersistentFlags().Int64Var(&opts.ChunkSize, "chunk-size", opts.ChunkSize, "Kubernetes list page size for list/scan/tui; set 0 to disable pagination")
	cmd.PersistentFlags().IntVar(&opts.Concurrency, "concurrency", defaultConcurrency(), "maximum number of HPAs to analyze in parallel for commands taking NAME [NAME...]; defaults to min(CPUs, client-go burst 10)")
	cmd.PersistentFlags().Float32Var(&opts.QPS, "qps", 0, "client-side rate limiting queries per second (0 uses client-go default)")
	cmd.PersistentFlags().IntVar(&opts.Burst, "burst", 0, "client-side rate limiting burst size (0 uses client-go default)")
	cmd.PersistentFlags().DurationVar(&opts.RequestTimeout, "request-timeout", opts.RequestTimeout, "timeout for each Kubernetes API request; set 0 to wait indefinitely")

}

// newWorkflowFlagSet builds the flags shared by the analysis commands: the
// mutation workflow (--apply and its safety switches), GitOps export,
// health-trend history, enrichment integrations, and standalone reports.
//
// These are attached to the commands that act on them rather than inherited
// by every command, so `version --policy-guard-mode=warn` is now rejected
// instead of silently accepted. The returned set is built once and shared by
// pointer across commands, which is exactly how cobra propagates persistent
// flags; only one command parses per invocation.
func newWorkflowFlagSet(opts *options) *pflag.FlagSet {
	fs := pflag.NewFlagSet("workflow", pflag.ContinueOnError)

	fs.BoolVar(&opts.Apply, "apply", false, "run suggested HPA spec patch workflow")
	fs.BoolVar(&opts.Diff, "diff", false, "show field-level diff of suggested changes")
	fs.BoolVar(&opts.DryRun, "dry-run", opts.DryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	fs.BoolVarP(&opts.Yes, "yes", "y", false, "skip confirmation when used with --apply")
	fs.BoolVar(&opts.AllowPartial, "allow-partial", false, "allow sequential (non-atomic) apply when patches cannot be merged; may leave the HPA partially modified")

	fs.StringVar(&opts.Export, "export", "", "export suggestions for GitOps: yaml, kustomize, or helm-values")

	fs.BoolVar(&opts.Trend, "trend", false, "show health score trend with flapping detection")
	fs.DurationVar(&opts.TrendSince, "trend-since", 24*time.Hour, "lookback window for health trend (default: 24h)")
	fs.DurationVar(&opts.TrendRetain, "trend-retain", 72*time.Hour, "retention period for health history (default: 72h)")

	fs.StringArrayVar(&opts.HealthWeightOverrides, "health-weight", nil, "override a health score penalty, for example scalingInactive=50; repeatable")
	fs.StringVar(&opts.KEDA, "keda", opts.KEDA, "KEDA ScaledObject enrichment: auto (enable when CRD present), on (force), off (disable)")
	fs.StringVar(&opts.VPA, "vpa", opts.VPA, "VPA conflict detection: auto (enable when CRD present), on (force), off (disable)")
	fs.StringVar(&opts.PolicyGuard, "policy-guard", "", "path to a policy file used to guard --apply patches")
	fs.StringVar(&opts.PolicyGuardMode, "policy-guard-mode", opts.PolicyGuardMode, "policy guard mode for --apply: block or warn")
	fs.StringVar(&opts.Report, "report", "", "generate standalone report: markdown, html, incident, junit, or sarif")

	// Preserve the historical boolean-style shorthand while keeping the values
	// tri-state: --keda/--vpa means on, and =auto|off stays available.
	if keda := fs.Lookup("keda"); keda != nil {
		keda.NoOptDefVal = "on"
	}
	if vpa := fs.Lookup("vpa"); vpa != nil {
		vpa.NoOptDefVal = "on"
	}
	return fs
}

// addSharedFlags copies each set onto the command's local flag set, skipping
// any flag whose name or shorthand the command already defines. Commands such
// as tui (--policy-guard) and timeline (--interval) register narrower,
// better-documented variants of a shared flag; those must win, and skipping
// also keeps this safe to call on a command more than once.
func addSharedFlags(cmd *cobra.Command, sets ...*pflag.FlagSet) {
	for _, fs := range sets {
		fs.VisitAll(func(f *pflag.Flag) {
			if cmd.Flags().Lookup(f.Name) != nil {
				return
			}
			if f.Shorthand != "" && cmd.Flags().ShorthandLookup(f.Shorthand) != nil {
				return
			}
			cmd.Flags().AddFlag(f)
		})
	}
}

func defaultConcurrency() int {
	return min(runtime.NumCPU(), 10)
}

// registerStatusFlags registers flags specific to the status / analyze command.
func registerStatusFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().Var(&opts.AnalysisProfile, "analysis-profile", "diagnostic preset: quick, standard, incident, doctor, metrics, capacity, readiness, deep")
	cmd.Flags().BoolVar(&opts.Interpret, "interpret", false, "include interpretation in status output")
	cmd.Flags().BoolVar(&opts.Explain, "explain", false, "include detailed interpretation and recommended actions")
	cmd.Flags().BoolVar(&opts.Suggest, "suggest", false, "include concrete suggestions for configuration changes")
	cmd.Flags().BoolVar(&opts.Fix, "fix", false, "show stronger fix plan with patch commands")
	cmd.Flags().BoolVar(&opts.NoInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	cmd.Flags().Var(&opts.Events, "events", "show recent HPA events: true, false, or a number")
	cmd.Flags().BoolVar(&opts.DiagnoseMetrics, "diagnose-metrics", false, "run comprehensive metrics pipeline health checks")
	cmd.Flags().BoolVar(&opts.MetricsFreshness, "metrics-freshness", false, "analyze per-metric data freshness, source, and staleness risk")
	cmd.Flags().BoolVar(&opts.CheckResources, "check-resources", false, "check HPA target utilization against pod resource requests")
	cmd.Flags().BoolVar(&opts.ExplainPods, "explain-pods", false, "analyze scale target pods for readiness, resource requests, and metric coverage")
	cmd.Flags().StringArrayVar(&opts.Simulate, "simulate", nil, "simulate HPA spec changes (e.g. maxReplicas=20); repeatable")
	cmd.Flags().StringArrayVar(&opts.SimulateMetric, "simulate-metric", nil, "simulate metric value changes (e.g. cpu=80%, memory=4Gi, http_requests=+20%); repeatable")
	cmd.Flags().Int32Var(&opts.SimulateDuration, "simulate-duration", 0, "duration in seconds for time-series projection in simulation (default: 0, disabled)")
	cmd.Flags().BoolVar(&opts.CapacityContext, "capacity-context", false, "check infrastructure capacity constraints affecting HPA scaling")
	cmd.Flags().BoolVar(&opts.CapacityHeadroom, "capacity-headroom", false, "estimate resource headroom needed to reach maxReplicas")
	cmd.Flags().BoolVar(&opts.ScalePath, "scale-path", false, "explain the path from HPA desired replicas to pods and scheduler capacity")
	cmd.Flags().BoolVar(&opts.DecisionTrace, "decision-trace", false, "show a step-by-step visible HPA decision trace")
	cmd.Flags().BoolVar(&opts.Rollout, "rollout", false, "include rollout-aware workload diagnosis")
	cmd.Flags().BoolVar(&opts.RolloutImpact, "rollout-impact", false, "show how Deployment/StatefulSet rollout state affects HPA scale-out")
	cmd.Flags().BoolVar(&opts.ReadinessImpact, "readiness-impact", false, "show how not-yet-ready pods and missing PodMetrics may affect HPA decisions")
	cmd.Flags().BoolVar(&opts.ScaleoutBlockers, "scaleout-blockers", false, "rank visible blockers preventing HPA scale-out from producing Ready pods")
	cmd.Flags().BoolVar(&opts.ControllerProfile, "controller-profile", false, "show HPA controller-manager timing assumptions used for interpretation")
	cmd.Flags().StringVar(&opts.AssumeProfile, "assume-profile", "", "assume a named HPA controller profile when controller-manager args are not visible")
	cmd.Flags().StringVar(&opts.ControllerProfileFile, "controller-profile-file", "", "path to an HPA controller profile YAML file")
	cmd.Flags().StringVar(&opts.Format, "format", "", "status output profile: structured")
	cmd.Flags().BoolVar(&opts.HiddenFactors, "hidden-factors", false, "show missing metrics, not-yet-ready pod, tolerance, and stabilization factors that are only partially visible")
	cmd.Flags().BoolVar(&opts.Deep, "deep", false, "enable capacity, rollout, readiness, and adapter diagnostics together (one-flag depth tier)")
	cmd.Flags().BoolVar(&opts.NoEnrich, "no-enrich", false, "disable all enrichment and show HPA-only status (RBAC-light; alias: --hpa-only)")
	cmd.Flags().BoolVar(&opts.HPAOnly, "hpa-only", false, "alias for --no-enrich; disable all enrichment and show HPA-only status")
	cmd.Flags().BoolVar(&opts.NodeAutoscaler, "node-autoscaler", false, "include Cluster Autoscaler scale-out context in status/doctor analysis")
	cmd.Flags().BoolVar(&opts.Karpenter, "karpenter", false, "include Karpenter-style node provisioning context in status/doctor analysis")
	cmd.Flags().BoolVar(&opts.ContextForAI, "context-for-ai", false, "emit a compact local-AI context pack instead of normal status text")
	cmd.Flags().StringVar(&opts.Ask, "ask", "", "include a local-AI question in the context pack; no external LLM call is made")
	cmd.Flags().BoolVar(&opts.CapacityDeep, "capacity-deep", false, "deep capacity analysis for scale-out blockers including node capacity and container failures")
	cmd.Flags().BoolVar(&opts.CapacityPlan, "capacity-plan", false, "run capacity plan analysis when HPA is at maxReplicas")
	cmd.Flags().Int32Var(&opts.TargetMax, "target-max", 0, "target maxReplicas for capacity plan (default: 2x current max, soft-capped at 200)")
	cmd.Flags().BoolVar(&opts.GitOpsCheck, "gitops-check", false, "detect GitOps manifest conflicts with HPA-managed replicas")
	cmd.Flags().StringVar(&opts.ManifestPath, "manifest", "", "path to manifest file or directory for GitOps conflict detection")
	cmd.Flags().BoolVar(&opts.MetricContract, "metric-contract", false, "verify HPA metric references are queryable from metrics APIs")
	cmd.Flags().BoolVar(&opts.ChurnDetect, "churn-detect", false, "detect replica thrashing and recommend stabilization adjustments")
	cmd.Flags().BoolVar(&opts.MetricHints, "metric-hints", false, "troubleshoot custom/external metric issues with common failure pattern hints")
	cmd.Flags().BoolVar(&opts.ContainerAdvisor, "container-advisor", false, "suggest ContainerResource metrics for multi-container HPA targets")
	cmd.Flags().BoolVar(&opts.BehaviorAdvisor, "behavior-advisor", false, "analyze behavior config and suggest stabilization/policy tuning")
	cmd.Flags().StringVar(&opts.DecisionTraceFormat, "decision-trace-format", "", "structured decision trace output format: text, json, or yaml")
	cmd.Flags().BoolVar(&opts.FlappingAdvisor, "flapping-advisor", false, "recommend stabilization window changes to reduce replica flapping")
	cmd.Flags().BoolVar(&opts.TrendAnomaly, "trend-anomaly", false, "detect anomalies in health score history (enabled by default with --trend)")
	cmd.Flags().StringVar(&opts.IncidentTemplate, "incident-template", "", "path to a custom incident report template file")
	cmd.Flags().BoolVar(&opts.AdapterDiagnostics, "adapter-diagnostics", false, "diagnose custom/external metrics adapter signals")
}

// newWatchFlagSet builds the flags specific to the watch / TUI commands. Only
// status, watch, and tui read these options, so they are attached to those
// commands (and to root, which runs status implicitly) rather than inherited
// by every subcommand.
func newWatchFlagSet(opts *options) *pflag.FlagSet {
	fs := pflag.NewFlagSet("watch", pflag.ContinueOnError)
	fs.BoolVarP(&opts.Watch.Watch, "watch", "w", false, "watch HPA status periodically")
	fs.BoolVar(&opts.Dashboard, "dashboard", false, "render watch output as a compact terminal dashboard")
	fs.DurationVar(&opts.WatchInterval, "interval", opts.WatchInterval, "watch refresh interval")
	fs.DurationVar(&opts.WatchTimeout, "timeout", 0, "stop watching after this duration")
	fs.StringVar(&opts.UntilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
	return fs
}

func analysisProfileCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return validAnalysisProfiles(), cobra.ShellCompDirectiveNoFileComp
}
