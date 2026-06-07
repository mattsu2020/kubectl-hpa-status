// Package cmd implements the CLI commands for kubectl-hpa-status.
package cmd

import "github.com/spf13/cobra"

// registerCommonFlags registers flags that are shared across all commands:
// Kubernetes connection, output formatting, language, and debug settings.
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
	cmd.PersistentFlags().Float32Var(&opts.qps, "qps", 0, "client-side rate limiting queries per second (0 uses client-go default)")
	cmd.PersistentFlags().IntVar(&opts.burst, "burst", 0, "client-side rate limiting burst size (0 uses client-go default)")
}

// registerStatusFlags registers flags for the status / analyze command:
// interpretation, suggestions, apply workflow, enrichment, diagnostics, and simulation.
func registerStatusFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().BoolVar(&opts.interpret, "interpret", false, "include interpretation in status output")
	cmd.PersistentFlags().BoolVar(&opts.explain, "explain", false, "include detailed interpretation and recommended actions")
	cmd.PersistentFlags().BoolVar(&opts.suggest, "suggest", false, "include concrete suggestions for configuration changes")
	cmd.PersistentFlags().BoolVar(&opts.fix, "fix", false, "show stronger fix plan with patch commands")
	cmd.PersistentFlags().BoolVar(&opts.diff, "diff", false, "show field-level diff of suggested changes")
	cmd.PersistentFlags().BoolVar(&opts.apply, "apply", false, "run suggested HPA spec patch workflow")
	cmd.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", opts.dryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	cmd.PersistentFlags().BoolVarP(&opts.yes, "yes", "y", false, "skip confirmation when used with --apply")
	cmd.PersistentFlags().StringArrayVar(&opts.healthWeightOverrides, "health-weight", nil, "override a health score penalty, for example scalingInactive=50; repeatable")
	cmd.PersistentFlags().BoolVar(&opts.recommend, "recommend", false, "alias for --suggest")
	cmd.PersistentFlags().BoolVar(&opts.noInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	cmd.PersistentFlags().Var(&opts.events, "events", "show recent HPA events: true, false, or a number")
	cmd.PersistentFlags().BoolVar(&opts.keda, "keda", true, "enable KEDA ScaledObject integration (auto-detected when CRD is present; use --keda=false to disable)")
	cmd.PersistentFlags().BoolVar(&opts.diagnoseMetrics, "diagnose-metrics", false, "run comprehensive metrics pipeline health checks")
	cmd.PersistentFlags().BoolVar(&opts.metricsFreshness, "metrics-freshness", false, "analyze per-metric data freshness, source, and staleness risk")
	cmd.PersistentFlags().BoolVar(&opts.vpa, "vpa", true, "detect VerticalPodAutoscaler conflicts (auto-detected when CRD is present; use --vpa=false to disable)")
	cmd.PersistentFlags().BoolVar(&opts.checkResources, "check-resources", false, "check HPA target utilization against pod resource requests")
	cmd.PersistentFlags().BoolVar(&opts.explainPods, "explain-pods", false, "analyze scale target pods for readiness, resource requests, and metric coverage")
	cmd.PersistentFlags().StringArrayVar(&opts.simulate, "simulate", nil, "simulate HPA spec changes (e.g. maxReplicas=20); repeatable")
	cmd.PersistentFlags().StringArrayVar(&opts.simulateMetric, "simulate-metric", nil, "simulate metric value changes (e.g. cpu=80%, memory=4Gi, http_requests=+20%); repeatable")
	cmd.PersistentFlags().BoolVar(&opts.capacityContext, "capacity-context", false, "check infrastructure capacity constraints affecting HPA scaling")
	cmd.PersistentFlags().BoolVar(&opts.scalePath, "scale-path", false, "explain the path from HPA desired replicas to pods and scheduler capacity")
	cmd.PersistentFlags().BoolVar(&opts.capacityDeep, "capacity-deep", false, "deep capacity analysis for scale-out blockers including node capacity and container failures")
	cmd.PersistentFlags().BoolVar(&opts.capacityPlan, "capacity-plan", false, "run capacity plan analysis when HPA is at maxReplicas")
	cmd.PersistentFlags().Int32Var(&opts.targetMax, "target-max", 0, "target maxReplicas for capacity plan (default: 2x current max, capped at 200)")
	cmd.PersistentFlags().StringVar(&opts.report, "report", "", "generate standalone report: markdown or html")
}

// registerWatchFlags registers flags specific to the watch / TUI commands.
func registerWatchFlags(cmd *cobra.Command, opts *options) {
	cmd.PersistentFlags().BoolVarP(&opts.watch, "watch", "w", false, "watch HPA status periodically")
	cmd.PersistentFlags().BoolVar(&opts.dashboard, "dashboard", false, "render watch output as a compact terminal dashboard")
	cmd.PersistentFlags().DurationVar(&opts.watchInterval, "interval", opts.watchInterval, "watch refresh interval")
	cmd.PersistentFlags().DurationVar(&opts.watchTimeout, "timeout", 0, "stop watching after this duration")
	cmd.PersistentFlags().StringVar(&opts.untilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
}
