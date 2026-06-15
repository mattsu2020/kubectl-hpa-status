// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import (
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// registerCommonFlags registers flags that are shared across all commands:
// Kubernetes connection, output formatting, language, debug settings, and the
// cross-command apply/diff/export, trend, and health-weight flags that list,
// watch, and other subcommands consume alongside status.
func registerCommonFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().StringVarP(&opts.namespace, "namespace", "n", "", "namespace")
	cmd.PersistentFlags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "list HPAs across all namespaces")
	cmd.PersistentFlags().StringVar(&opts.contextName, "context", "", "kubeconfig context")
	cmd.PersistentFlags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	cmd.PersistentFlags().StringVar(&opts.cluster, "cluster", "", "kubeconfig cluster")
	cmd.PersistentFlags().StringVarP(&opts.output, "output", "o", "", "output format: table, wide, json, yaml, jsonpath=..., template=...")
	cmd.PersistentFlags().StringVar(&opts.template, "template", "", "template string to use when -o jsonpath or -o go-template/template is specified")
	cmd.PersistentFlags().BoolVar(&opts.wide, "wide", false, "show additional columns in table output")
	cmd.PersistentFlags().StringVarP(&opts.selector, "selector", "l", "", "label selector for list and scan, for example app=web,tier!=canary")
	cmd.PersistentFlags().StringVar(&opts.color, "color", opts.color, "colorize table output: auto, always, never")
	cmd.PersistentFlags().StringVar(&opts.lang, "lang", "", "text output language: en or ja")
	cmd.PersistentFlags().BoolVarP(&opts.debug, "debug", "v", false, "include internal analysis details such as ratios and health scoring inputs")
	cmd.PersistentFlags().StringVar(&opts.config, "config", "", "optional config file for analysis settings such as health score weights")
	cmd.PersistentFlags().Int64Var(&opts.chunkSize, "chunk-size", opts.chunkSize, "Kubernetes list page size for list/scan/tui; set 0 to disable pagination")
	cmd.PersistentFlags().IntVar(&opts.concurrency, "concurrency", runtime.NumCPU(), "maximum number of HPAs to analyze in parallel for multi-HPA status/timeline; defaults to the number of CPUs")
	cmd.PersistentFlags().Float32Var(&opts.qps, "qps", 0, "client-side rate limiting queries per second (0 uses client-go default)")
	cmd.PersistentFlags().IntVar(&opts.burst, "burst", 0, "client-side rate limiting burst size (0 uses client-go default)")

	// Apply / suggestion workflow (shared by status, list, watch).
	cmd.PersistentFlags().BoolVar(&opts.apply, "apply", false, "run suggested HPA spec patch workflow")
	cmd.PersistentFlags().BoolVar(&opts.diff, "diff", false, "show field-level diff of suggested changes")
	cmd.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", opts.dryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	cmd.PersistentFlags().BoolVarP(&opts.yes, "yes", "y", false, "skip confirmation when used with --apply")
	cmd.PersistentFlags().BoolVar(&opts.allowPartial, "allow-partial", false, "allow sequential (non-atomic) apply when patches cannot be merged; may leave the HPA partially modified")

	// GitOps export (shared by status, export commands).
	cmd.PersistentFlags().StringVar(&opts.export, "export", "", "export suggestions for GitOps: yaml, kustomize, or helm-values")
	cmd.PersistentFlags().StringVar(&opts.exportPatch, "export-patch", "", "alias for --export; formats: yaml, kustomize, helm-values")

	// Health-score trend history (shared by status, history, timeline).
	cmd.PersistentFlags().BoolVar(&opts.trend, "trend", false, "show health score trend with flapping detection")
	cmd.PersistentFlags().DurationVar(&opts.trendSince, "trend-since", 24*time.Hour, "lookback window for health trend (default: 24h)")
	cmd.PersistentFlags().DurationVar(&opts.trendRetain, "trend-retain", 72*time.Hour, "retention period for health history (default: 72h)")

	// Health-score weighting (consumed by status, list, scan, watch, etc.).
	cmd.PersistentFlags().StringArrayVar(&opts.healthWeightOverrides, "health-weight", nil, "override a health score penalty, for example scalingInactive=50; repeatable")
}

// registerStatusFlags registers flags specific to the status / analyze command:
// interpretation, enrichment, diagnostics, and simulation. Apply/diff/export,
// trend, and health-weight flags are registered by registerCommonFlags because
// they are shared across commands.
func registerStatusFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().BoolVar(&opts.interpret, "interpret", false, "include interpretation in status output")
	cmd.Flags().BoolVar(&opts.explain, "explain", false, "include detailed interpretation and recommended actions")
	cmd.Flags().BoolVar(&opts.suggest, "suggest", false, "include concrete suggestions for configuration changes")
	cmd.Flags().BoolVar(&opts.fix, "fix", false, "show stronger fix plan with patch commands")
	cmd.Flags().BoolVar(&opts.recommend, "recommend", false, "alias for --suggest")
	cmd.Flags().BoolVar(&opts.noInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	cmd.Flags().Var(&opts.events, "events", "show recent HPA events: true, false, or a number")
	cmd.Flags().StringVar(&opts.keda, "keda", "auto", "KEDA ScaledObject enrichment: auto (enable when CRD present), on (force), off (disable)")
	cmd.Flags().BoolVar(&opts.diagnoseMetrics, "diagnose-metrics", false, "run comprehensive metrics pipeline health checks")
	cmd.Flags().BoolVar(&opts.metricsFreshness, "metrics-freshness", false, "analyze per-metric data freshness, source, and staleness risk")
	cmd.Flags().StringVar(&opts.vpa, "vpa", "auto", "VPA conflict detection: auto (enable when CRD present), on (force), off (disable)")
	cmd.Flags().BoolVar(&opts.checkResources, "check-resources", false, "check HPA target utilization against pod resource requests")
	cmd.Flags().BoolVar(&opts.explainPods, "explain-pods", false, "analyze scale target pods for readiness, resource requests, and metric coverage")
	cmd.Flags().StringArrayVar(&opts.simulate, "simulate", nil, "simulate HPA spec changes (e.g. maxReplicas=20); repeatable")
	cmd.Flags().StringArrayVar(&opts.simulateMetric, "simulate-metric", nil, "simulate metric value changes (e.g. cpu=80%, memory=4Gi, http_requests=+20%); repeatable")
	cmd.Flags().Int32Var(&opts.simulateDuration, "simulate-duration", 0, "duration in seconds for time-series projection in simulation (default: 0, disabled)")
	cmd.Flags().BoolVar(&opts.capacityContext, "capacity-context", false, "check infrastructure capacity constraints affecting HPA scaling")
	cmd.Flags().BoolVar(&opts.capacityHeadroom, "capacity-headroom", false, "estimate resource headroom needed to reach maxReplicas")
	cmd.Flags().BoolVar(&opts.scalePath, "scale-path", false, "explain the path from HPA desired replicas to pods and scheduler capacity")
	cmd.Flags().BoolVar(&opts.decisionTrace, "decision-trace", false, "show a step-by-step visible HPA decision trace")
	cmd.Flags().BoolVar(&opts.rollout, "rollout", false, "include rollout-aware workload diagnosis")
	cmd.Flags().BoolVar(&opts.rolloutImpact, "rollout-impact", false, "show how Deployment/StatefulSet rollout state affects HPA scale-out")
	cmd.Flags().BoolVar(&opts.readinessImpact, "readiness-impact", false, "show how not-yet-ready pods and missing PodMetrics may affect HPA decisions")
	cmd.Flags().BoolVar(&opts.scaleoutBlockers, "scaleout-blockers", false, "rank visible blockers preventing HPA scale-out from producing Ready pods")
	cmd.Flags().BoolVar(&opts.controllerProfile, "controller-profile", false, "show HPA controller-manager timing assumptions used for interpretation")
	cmd.Flags().StringVar(&opts.assumeProfile, "assume-profile", "", "assume a named HPA controller profile when controller-manager args are not visible")
	cmd.Flags().StringVar(&opts.controllerProfileFile, "controller-profile-file", "", "path to an HPA controller profile YAML file")
	cmd.Flags().StringVar(&opts.format, "format", "", "status output profile: structured")
	cmd.Flags().BoolVar(&opts.hiddenFactors, "hidden-factors", false, "show missing metrics, not-yet-ready pod, tolerance, and stabilization factors that are only partially visible")
	cmd.Flags().BoolVar(&opts.nodeAutoscaler, "node-autoscaler", false, "include Cluster Autoscaler scale-out context in status/doctor analysis")
	cmd.Flags().BoolVar(&opts.karpenter, "karpenter", false, "include Karpenter-style node provisioning context in status/doctor analysis")
	cmd.Flags().BoolVar(&opts.contextForAI, "context-for-ai", false, "emit a compact local-AI context pack instead of normal status text")
	cmd.Flags().StringVar(&opts.ask, "ask", "", "include a local-AI question in the context pack; no external LLM call is made")
	cmd.Flags().BoolVar(&opts.capacityDeep, "capacity-deep", false, "deep capacity analysis for scale-out blockers including node capacity and container failures")
	cmd.Flags().BoolVar(&opts.capacityPlan, "capacity-plan", false, "run capacity plan analysis when HPA is at maxReplicas")
	cmd.Flags().Int32Var(&opts.targetMax, "target-max", 0, "target maxReplicas for capacity plan (default: 2x current max, capped at 200)")
	cmd.Flags().StringVar(&opts.report, "report", "", "generate standalone report: markdown, html, incident, junit, or sarif")
	cmd.Flags().BoolVar(&opts.gitopsCheck, "gitops-check", false, "detect GitOps manifest conflicts with HPA-managed replicas")
	cmd.Flags().StringVar(&opts.manifestPath, "manifest", "", "path to manifest file or directory for GitOps conflict detection")
	cmd.Flags().BoolVar(&opts.metricContract, "metric-contract", false, "verify HPA metric references are queryable from metrics APIs")
	cmd.Flags().BoolVar(&opts.churnDetect, "churn-detect", false, "detect replica thrashing and recommend stabilization adjustments")
	cmd.Flags().BoolVar(&opts.metricHints, "metric-hints", false, "troubleshoot custom/external metric issues with common failure pattern hints")
	cmd.Flags().BoolVar(&opts.containerAdvisor, "container-advisor", false, "suggest ContainerResource metrics for multi-container HPA targets")
	cmd.Flags().BoolVar(&opts.behaviorAdvisor, "behavior-advisor", false, "analyze behavior config and suggest stabilization/policy tuning")
	cmd.Flags().StringVar(&opts.decisionTraceFormat, "decision-trace-format", "", "structured decision trace output format: text, json, or yaml")
	cmd.Flags().BoolVar(&opts.flappingAdvisor, "flapping-advisor", false, "recommend stabilization window changes to reduce replica flapping")
	cmd.Flags().BoolVar(&opts.trendAnomaly, "trend-anomaly", false, "detect anomalies in health score history (enabled by default with --trend)")
	cmd.Flags().StringVar(&opts.incidentTemplate, "incident-template", "", "path to a custom incident report template file")
	cmd.Flags().StringVar(&opts.policyGuard, "policy-guard", "", "path to a policy file used to guard --apply patches")
	cmd.Flags().StringVar(&opts.policyGuardMode, "policy-guard-mode", "block", "policy guard mode for --apply: block or warn")
	cmd.Flags().BoolVar(&opts.adapterDiagnostics, "adapter-diagnostics", false, "diagnose custom/external metrics adapter signals")
}

// registerWatchFlags registers flags specific to the watch / TUI commands.
func registerWatchFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().BoolVarP(&opts.watch, "watch", "w", false, "watch HPA status periodically")
	cmd.PersistentFlags().BoolVar(&opts.dashboard, "dashboard", false, "render watch output as a compact terminal dashboard")
	cmd.PersistentFlags().DurationVar(&opts.watchInterval, "interval", opts.watchInterval, "watch refresh interval")
	cmd.PersistentFlags().DurationVar(&opts.watchTimeout, "timeout", 0, "stop watching after this duration")
	cmd.PersistentFlags().StringVar(&opts.untilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
}
