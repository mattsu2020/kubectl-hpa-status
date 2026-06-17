// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"runtime"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/cmdoptions"
	"github.com/spf13/cobra"
)

// registerCommonFlags registers flags that are shared across all commands.
func registerCommonFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().StringVarP(&opts.Namespace, "namespace", "n", "", "namespace")
	cmd.PersistentFlags().BoolVarP(&opts.AllNamespaces, "all-namespaces", "A", false, "list HPAs across all namespaces")
	cmd.PersistentFlags().StringVar(&opts.ContextName, "context", "", "kubeconfig context")
	cmd.PersistentFlags().StringVar(&opts.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
	cmd.PersistentFlags().StringVar(&opts.Cluster, "cluster", "", "kubeconfig cluster")
	cmd.PersistentFlags().StringVarP(&opts.Output, "output", "o", "", "output format: table, wide, json, yaml, jsonpath=..., template=...")
	cmd.PersistentFlags().StringVar(&opts.Template, "template", "", "template string to use when -o jsonpath or -o go-template/template is specified")
	cmd.PersistentFlags().BoolVar(&opts.Wide, "wide", false, "show additional columns in table output")
	cmd.PersistentFlags().StringVarP(&opts.Selector, "selector", "l", "", "label selector for list and scan, for example app=web,tier!=canary")
	cmd.PersistentFlags().StringVar(&opts.Color, "color", opts.Color, "colorize table output: auto, always, never")
	cmd.PersistentFlags().StringVar(&opts.Lang, "lang", "", "text output language: en or ja")
	cmd.PersistentFlags().BoolVarP(&opts.Debug, "debug", "v", false, "include internal analysis details such as ratios and health scoring inputs")
	cmd.PersistentFlags().StringVar(&opts.Config, "config", "", "optional config file for analysis settings such as health score weights")
	cmd.PersistentFlags().Int64Var(&opts.ChunkSize, "chunk-size", opts.ChunkSize, "Kubernetes list page size for list/scan/tui; set 0 to disable pagination")
	cmd.PersistentFlags().IntVar(&opts.Concurrency, "concurrency", runtime.NumCPU(), "maximum number of HPAs to analyze in parallel for multi-HPA status/timeline; defaults to the number of CPUs")
	cmd.PersistentFlags().Float32Var(&opts.QPS, "qps", 0, "client-side rate limiting queries per second (0 uses client-go default)")
	cmd.PersistentFlags().IntVar(&opts.Burst, "burst", 0, "client-side rate limiting burst size (0 uses client-go default)")

	cmd.PersistentFlags().BoolVar(&opts.Apply, "apply", false, "run suggested HPA spec patch workflow")
	cmd.PersistentFlags().BoolVar(&opts.Diff, "diff", false, "show field-level diff of suggested changes")
	cmd.PersistentFlags().BoolVar(&opts.DryRun, "dry-run", opts.DryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	cmd.PersistentFlags().BoolVarP(&opts.Yes, "yes", "y", false, "skip confirmation when used with --apply")
	cmd.PersistentFlags().BoolVar(&opts.AllowPartial, "allow-partial", false, "allow sequential (non-atomic) apply when patches cannot be merged; may leave the HPA partially modified")

	cmd.PersistentFlags().StringVar(&opts.Export, "export", "", "export suggestions for GitOps: yaml, kustomize, or helm-values")
	cmd.PersistentFlags().StringVar(&opts.ExportPatch, "export-patch", "", "alias for --export; formats: yaml, kustomize, helm-values")

	cmd.PersistentFlags().BoolVar(&opts.Trend, "trend", false, "show health score trend with flapping detection")
	cmd.PersistentFlags().DurationVar(&opts.TrendSince, "trend-since", 24*time.Hour, "lookback window for health trend (default: 24h)")
	cmd.PersistentFlags().DurationVar(&opts.TrendRetain, "trend-retain", 72*time.Hour, "retention period for health history (default: 72h)")

	cmd.PersistentFlags().StringArrayVar(&opts.HealthWeightOverrides, "health-weight", nil, "override a health score penalty, for example scalingInactive=50; repeatable")
}

// registerStatusFlags registers flags specific to the status / analyze command.
func registerStatusFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().Var(&opts.AnalysisProfile, "analysis-profile", "diagnostic preset: quick, standard, incident, doctor, metrics, capacity, readiness")
	cmd.Flags().BoolVar(&opts.Interpret, "interpret", false, "include interpretation in status output")
	cmd.Flags().BoolVar(&opts.Explain, "explain", false, "include detailed interpretation and recommended actions")
	cmd.Flags().BoolVar(&opts.Suggest, "suggest", false, "include concrete suggestions for configuration changes")
	cmd.Flags().BoolVar(&opts.Fix, "fix", false, "show stronger fix plan with patch commands")
	cmd.Flags().BoolVar(&opts.Recommend, "recommend", false, "alias for --suggest")
	cmd.Flags().BoolVar(&opts.NoInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	cmd.Flags().Var(&opts.Events, "events", "show recent HPA events: true, false, or a number")
	cmd.Flags().StringVar(&opts.KEDA, "keda", "auto", "KEDA ScaledObject enrichment: auto (enable when CRD present), on (force), off (disable)")
	cmd.Flags().BoolVar(&opts.DiagnoseMetrics, "diagnose-metrics", false, "run comprehensive metrics pipeline health checks")
	cmd.Flags().BoolVar(&opts.MetricsFreshness, "metrics-freshness", false, "analyze per-metric data freshness, source, and staleness risk")
	cmd.Flags().StringVar(&opts.VPA, "vpa", "auto", "VPA conflict detection: auto (enable when CRD present), on (force), off (disable)")
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
	cmd.Flags().BoolVar(&opts.NodeAutoscaler, "node-autoscaler", false, "include Cluster Autoscaler scale-out context in status/doctor analysis")
	cmd.Flags().BoolVar(&opts.Karpenter, "karpenter", false, "include Karpenter-style node provisioning context in status/doctor analysis")
	cmd.Flags().BoolVar(&opts.ContextForAI, "context-for-ai", false, "emit a compact local-AI context pack instead of normal status text")
	cmd.Flags().StringVar(&opts.Ask, "ask", "", "include a local-AI question in the context pack; no external LLM call is made")
	cmd.Flags().BoolVar(&opts.CapacityDeep, "capacity-deep", false, "deep capacity analysis for scale-out blockers including node capacity and container failures")
	cmd.Flags().BoolVar(&opts.CapacityPlan, "capacity-plan", false, "run capacity plan analysis when HPA is at maxReplicas")
	cmd.Flags().Int32Var(&opts.TargetMax, "target-max", 0, "target maxReplicas for capacity plan (default: 2x current max, capped at 200)")
	cmd.Flags().StringVar(&opts.Report, "report", "", "generate standalone report: markdown, html, incident, junit, or sarif")
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
	cmd.Flags().StringVar(&opts.PolicyGuard, "policy-guard", "", "path to a policy file used to guard --apply patches")
	cmd.Flags().StringVar(&opts.PolicyGuardMode, "policy-guard-mode", "block", "policy guard mode for --apply: block or warn")
	cmd.Flags().BoolVar(&opts.AdapterDiagnostics, "adapter-diagnostics", false, "diagnose custom/external metrics adapter signals")
}

// registerWatchFlags registers flags specific to the watch / TUI commands.
func registerWatchFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().BoolVarP(&opts.Watch.Watch, "watch", "w", false, "watch HPA status periodically")
	cmd.PersistentFlags().BoolVar(&opts.Dashboard, "dashboard", false, "render watch output as a compact terminal dashboard")
	cmd.PersistentFlags().DurationVar(&opts.WatchInterval, "interval", opts.WatchInterval, "watch refresh interval")
	cmd.PersistentFlags().DurationVar(&opts.WatchTimeout, "timeout", 0, "stop watching after this duration")
	cmd.PersistentFlags().StringVar(&opts.UntilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
}

func analysisProfileCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return cmdoptions.ValidAnalysisProfiles(), cobra.ShellCompDirectiveNoFileComp
}
