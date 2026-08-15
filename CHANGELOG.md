# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The unreleased line ships as **3.0.0** (major): it executes the v3 breaking
changes decided in `ROADMAP.md` — deprecated-facade removal, CLI surface
conservation under workflow parents, and the v2 default wire schema. The
migration notes are in the breaking sections below.

### Removed (breaking)

- **Removed all 57 deprecated compatibility facades from `pkg/hpa`.** These
  were `Deprecated:`-marked re-exports of the canonical domain sub-packages
  and existed only for external importers through v2. Import the canonical
  packages directly:

  | Removed `pkg/hpa` symbols | Replacement |
  | --- | --- |
  | `SimulationResult`, `SimulationState`, `SimulationExtendedOptions`, `ProjectedState`, `SimulateHPA`, `SimulateScenario`, `SimulateExtended`, `SimulateMetricChange`, `BuildSimulatedHPA`, `FormatTrajectoryASCII`, `ErrInvalidSimulationValue`, `ErrUnsupportedSimulationSemantics` | `pkg/hpa/simulate` |
  | `VPAInfo`, `VPAContainerPolicy`, `VPARecommendationInfo`, `VPAConflictInfo`, `VPARecommendation`, `VPAConflictLevel`, `VPAAdvisory`, `VPAConflictNone/Warning/Error`, `AnalyzeVPA`, `NewVPAConflictInfo`, `AnalyzeVPAAdvisory` | `pkg/hpa/vpa` (`Info`, `ContainerPolicy`, `ConflictInfo`, `Advisory`, `Analyze`, `NewConflictInfo`, `AnalyzeAdvisory`, ...) |
  | `HealthSnapshot`, `HealthTrendResult`, `AnalyzeHealthTrend`, `DetectFlapping`, `ComputeHealthVariance`, `FormatHealthSparkline`, `DetectAnomalies`, `RenderHealthTrendASCII`, `FormatTrendText`, `FormatTrendAnomalyText`, `FormatTrendAnomalyGraph`, `FormatTrendListRow` | `pkg/hpa/healthtrend` (`HealthSnapshot`, `Result`, `AnalyzeHealthTrend`, ...) |
  | `ReadinessImpact`, `ReadinessDoctorReport`, `ReadinessPodAgeDistribution`, `ReadinessProbeAnalysis`, `ReadinessInitImpact`, `ReadinessExclusionEstimate`, `ReadinessDoctorInput`, `ReadinessDoctorPod`, `AnalyzeReadinessDoctor` | `pkg/hpa/readiness` (`Impact`, `DoctorReport`, `ProbeAnalysis`, ...) |
  | `ChurnLevel`, `ChurnAnalysis`, `ChurnRecommendation`, `ChurnLow/Medium/High/Critical`, `AnalyzeChurnFromEvents`, `AnalyzeChurnFromSnapshots` | `pkg/hpa/churn` |
  | `KEDAAnalysis`, `KEDATriggerSummary`, `KEDAFallbackInfo`, `AnalyzeKEDA` | `pkg/hpa/keda` (`Analysis`, `TriggerSummary`, `FallbackInfo`, `Analyze`) |
  | `WriteMarkdownListReport`, `WriteHTMLListReport` | `pkg/hpa/render` |
  | `healthtrend.HealthTrendResult` | `healthtrend.Result` (in-package rename) |

  The `scripts/check-deprecated-facades` checker, its `make facade-check`
  target, and its CI step were removed together with the facades.

### Changed (breaking)

- **Grouped the focused diagnosis preset commands under their workflow
  parents.** `doctor` now hosts `doctor readiness` (ex-`readiness`),
  `doctor rollout` (ex-`rollout-context`), `doctor capacity`
  (ex-`node-context`), `doctor trace` (ex-`trace`), `doctor path` (ex-`path`),
  and `doctor preflight` (ex-`preflight`); `container-advisor` maps to the
  existing `advisor container`. The historical top-level names keep working
  for the whole v3 line as hidden deprecated aliases that print a one-line
  migration hint on stderr, and will be removed in the next major release.
  `doctor NAME` itself is unchanged. New presets must ship as a `status` flag
  or `--analysis-profile` value first.
- **Structured status/watch output now defaults to the grouped v2 schema.**
  `status -o json|yaml|jsonl|jsonpath|go-template` emits the v2 projection
  (`apiVersion: "hpa-status/v2"` with 13 nested group views) by default; pass
  `--output-schema=v1` to keep the flat legacy shape. Text output is
  schema-independent and unchanged. JSONL consumers parsing the v1 one-line
  success array must switch to v2 per-line records or pin `v1`.

### Fixed

- **`readiness-doctor` no longer panics when the discovery client has no REST
  client.** `countMissingMetrics` called `Discovery().RESTClient().Get()`
  without the nil guard its two sibling call sites
  (`fetchPodMetricSamples`, `fetchPodMetricNames`) already had, so any client
  built without a REST config crashed the command with a nil-pointer
  dereference instead of degrading to "no pods known to be missing metrics".
- **`--controller-profile-file` now reports the file as the profile source.**
  `loadControllerProfileFile` seeded the profile from
  `DefaultControllerProfile()`, which sets `Source: "defaults"`, so the
  `if profile.Source == ""` fallback was unreachable and a file-loaded profile
  claimed its values came from Kubernetes defaults — exactly the thing the flag
  exists to let an operator verify.

### Changed

- **`--concurrency` now applies to every command that takes `NAME [NAME...]`.**
  Only `status` read the flag; `blockers`, `capacity-plan`, `why-not-scale`,
  `rollout`, `ownership`, `assumptions`, and `autoscaler-map` fetched their
  HPAs one at a time regardless of the setting. They now share the bounded
  parallel helper in `cmd/parallel.go`, which preserves the previous error
  contract: the first failure in *input* order is returned and no partial
  output is rendered, independent of goroutine scheduling. The flag's help text
  claimed it covered "status/timeline"; `timeline` takes a single NAME and never
  read it.
- `autoscaler-map`, `why-not-scale`, and `rollout` build their Kubernetes client
  once per run instead of once per HPA name, which previously re-read and
  re-parsed the kubeconfig for every name in the argument list.
- **Workflow and watch flags are now scoped to the commands that use them.**
  `--apply`, `--diff`, `--dry-run`, `--yes`, `--allow-partial`, `--export`,
  `--trend*`, `--health-weight`, `--keda`, `--vpa`, `--policy-guard*`,
  `--report`, and the watch flags (`--watch`, `--dashboard`, `--interval`,
  `--timeout`, `--until-condition`) were registered as root persistent flags,
  so every subcommand accepted them — `version --policy-guard-mode=warn` exited
  0 with the flag silently ignored. They are now attached to the analysis
  commands that read them (and to the root, which runs `status` implicitly),
  and are rejected elsewhere. `version --help` and `completion --help` drop
  from 40 flags to 20. No flag was removed from a command that acts on it.
- `--lang` help text now states its actual scope: it localizes table/section
  labels and the status summary line, while analysis detail text stays English.
- Minimum Go version relaxed from `1.26.5` to `1.26.0`, so consumers importing
  `pkg/hpa` are not forced onto a specific patch release.
- Coverage thresholds in `scripts/check-coverage.sh` raised to sit ~2 points
  below actual coverage. They had drifted 10+ points below, which let a large
  regression pass the gate.
- Refactored analysis, observation, enrichment, history, conversion, and TUI
  boundaries so commands share canonical request-scoped services and models.
- Capacity planning now reports unknown inputs explicitly, accounts for Pod
  slots and scheduling constraints, and keeps Cluster Autoscaler detection
  advisory until compatible node-group headroom is proven.
- HPA/VPA conflict detection now distinguishes utilization targets from
  average-value targets, and retrospective analysis preserves complete
  replica ranges and terminal bottleneck duration.

- **`bundle`, `incident-bundle`, and `snapshot` now redact by default.**
  These commands produce evidence packs meant to leave the operator's machine,
  so `--redact` defaults to `true` (matching `support-bundle`). Pass
  `--redact=false` only for trusted local archives that require exact object
  values.

### Added

- Opt-in grouped status schema v2 for JSON, YAML, JSONL, JSONPath, and Go
  templates, including per-item error records and a checked-in JSON Schema.

- **Input size guardrails.** Files read fully into memory (candidate HPA
  manifests, recorded JSON traces, GitOps manifests, config files) are capped
  at 50 MiB, and JSONL record streaming is capped at 1,000,000 snapshots per
  HPA to prevent out-of-memory aborts on pathologically large inputs.
- **Directory walk limits for `lint` and `gitops review`.** Walks are bounded
  to depth 20 and 10,000 files and skip `.git`, `node_modules`, and `vendor`
  directories, so a stray `lint /` no longer scans an entire filesystem.
- **Symlink-safe bundle output.** Bundle and snapshot writers refuse to write
  through an existing symlink at the output path.
- **Matchable simulation errors.** `pkg/hpa` now exports
  `ErrUnsupportedSimulationPath` and `ErrInvalidSimulationValue` so callers
  can branch with `errors.Is` instead of matching message text.

### Fixed

- `ai-context` output now sanitizes cluster-controlled condition/metric text
  so ANSI escape sequences cannot spoof the terminal.

### Internal

- **Coverage gates now have root-package floors.** The `cmd` and `pkg/hpa`
  checks matched by substring, so they also counted every subpackage under
  those paths. As the package splits progressed, each freshly extracted
  subpackage landed with high coverage and lifted the aggregate: the `cmd`
  tree read 66.3% while the flat `cmd` package was 62.7%, and the `pkg/hpa`
  tree read 80.2% against a 75.5% root. A regression confined to either root
  could pass its gate. Added `cmd (root package)` and `pkg/hpa (root package)`
  checks anchored with `[^/]+\.go:`.
- **`cmd/` sub-package extraction (phase 2).** Extracted three of the four
  shallow command groups named in ROADMAP.md: `cmd/internal/compat` (report
  model, discovery rules, text renderer), `cmd/internal/alerts` (Prometheus and
  Datadog rule templates), and `cmd/internal/buildinfo` (ldflags/build-info
  version resolution). All three are cobra-free and option-free and are covered
  at 90–100%. `completion` remains blocked on narrowing its `*options` and
  flag-vocabulary dependencies into an explicit contract.
- **Split `pkg/hpa/capacity_plan.go`** (1258 lines, the only production file
  over the 800-line guidance) into `capacity_plan.go` (694),
  `capacity_plan_quota.go` (quota and LimitRange checks), and
  `capacity_plan_node.go` (node headroom, Pod slots, fragmentation). Decomposed
  `checkNodeCapacity` (cyclomatic complexity 33) and
  `allContainersSpecifyQuotaResource` (18) into per-dimension helpers, removing
  both `//nolint:gocyclo` suppressions.
- **Added fleet-path benchmarks** (`internal/analysis/bench_test.go`) for
  `AnalyzeBatch` at 100/500/1000 HPAs, with and without enrichment, plus
  `WriteListText` in normal and wide modes. The `list`/`scan`/`tui` path had no
  benchmark despite being the code that runs against large clusters.
- Raised `cmd` root-package coverage from 62.7% to 69.0% (total 76.5% → 78.2%)
  with smoke tests for the eight least-covered user-facing commands: `compare`
  (3.5% → 76.6%), `gitops review` (12.5% → 80.2%), `readiness-doctor`
  (12.5% → 60.3%), `snapshot` (15.7% → 71.8%), the controller-profile helper
  (19.2% → 88.1%), `history` (21.4% → 80.0%), `incident-bundle`
  (27.8% → 77.8%), and `flap` (29.3% → 91.8%). The two bugs listed under Fixed
  above were found by these tests.
- Deduplicated the enrichment mode check: `internal/enrichment.Requested` is
  now the single definition shared with `cmd`.
- Unified the previously duplicated `filepath.Walk` collectors of `lint` and
  `gitops review` into one bounded `collectManifestFiles` helper.
- Removed the unused `kubernetes.Interface` parameter from
  `kube.FindScaledObjectForHPA`.
- Raised `pkg/hpa` test coverage from 70.0% to ~74% (cluster diagnostics,
  behavior/container advisor text renderers, autoscaler map text, churn
  facades, structured decision trace text).

## [2.0.0] - 2026-07-13

This release removes deprecated command/flag aliases and lands the first
structural refactors of the `cmd/` package and the `Analysis` model. See the
migration notes below; most users only need to update scripts that used the
removed aliases.

### Removed (breaking)
- Removed the `analyze` subcommand (alias `diagnose`). Use `status NAME --explain` instead.
- Removed the historical top-level aliases for alpha-grouped commands: `policy`, `gitops`, `bundle`, `incident-bundle`, `support-bundle`, `capacity`, `capacity-gap`, `autoscaler-map`, `analyze-record`, `flap`. Use the `alpha <cmd>` form (e.g. `alpha policy`, `alpha bundle`).
- Removed the `--recommend` flag. Use `--suggest`.
- Removed the `--export-patch` flag. Use `--export`.
- Removed the `--max-score` list flag. Use `--health-score`.

### Changed
- Multi-HPA `status NAME1 NAME2 ...` now produces **partial results**: a per-item fetch/build failure no longer aborts the whole batch. Successful items are rendered normally, and the failed item is surfaced as an error entry in the output.
- Exit code for multi-HPA runs now reflects the most severe per-item outcome: any build failure → exit `1` (`error`), otherwise any warning-health item → exit `2` (`warning`), otherwise exit `0`. Previously the first failing HPA aborted the whole run with no output.
- Guarded `*deploy.Spec.Replicas` / `*sts.Spec.Replicas` dereferences in the GitOps conflict path against nil (defaults to 1, matching the Kubernetes default).
- Deduplicated the "record file has no snapshots" error across the timeline/replay loaders and wrapped it in a new `ErrNoRecordedSnapshots` sentinel so callers can match via `errors.Is`. Added `ErrPolicyViolations`, `ErrPolicyGuardBlocked`, and `ErrInvalidCandidateSpec` sentinels for the corresponding exit paths.

### Changed (breaking)
- `status NAME1 NAME2 -o json` and `-o yaml` now emit a `StatusBatch` envelope instead of a bare `[]StatusReport` array. New shape:
  ```json
  {
    "apiVersion": "hpa-status/v1",
    "items": [
      {"namespace": "default", "name": "web", "status": "ok", "report": { ... }},
      {"namespace": "default", "name": "missing", "status": "error", "error": "HPA \"missing\" was not found ..."}
    ]
  }
  ```
  Single-HPA `status NAME -o json` is unchanged (still a bare `StatusReport`). Failed items have `status: "error"`, a non-empty `error` string, and no `report` field.

### Added
- `Analysis` group-view methods (`Meta`, `Replicas`, `Decision`, `MetricsGroup`, `ConditionsGroup`, `ActionsGroup`, `Lifecycle`) as the first step of the additive grouping migration. The flat fields and their JSON shape are unchanged; new code can reach related fields through these read-only views.
- New `cmd/internal/{errs,client,output}` and `cmd/bundle` sub-packages as the first extractions from the monolithic `cmd/` package, following the facade-then-migrate pattern.
- Test coverage lifted across `cmd/` (12 previously-untested files), `internal/cmdoptions` (34.9% → 61.2%), `pkg/hpa/keda` (45.8% → 96.6%), and other low-coverage packages.
- Split the 1934-line `test/e2e/e2e_test.go` into per-area files (`e2e_status_test.go`, `e2e_list_scan_test.go`, `e2e_config_test.go`, `e2e_keda_test.go`, `e2e_watch_tui_test.go`, plus shared `e2e_helpers_test.go`).
- Streaming output for the `list` command on large clusters, plus refined status display.
- Single-HPA `status -o json`/`-o yaml` reports now include an `apiVersion` field.
- `LDFLAGS` Makefile variable (default `-s -w`) so local builds match release stripping; override with `LDFLAGS=` for debug builds.

## [0.10.0] - 2026-06-13

### Added
- Added visualization, real-time monitoring, and explainability features across the analysis pipeline.
- Added a `bundle` subcommand and an incident report output format with policy guard, adapter diagnostics, and rich incident Markdown.
- Added readiness, rollout, scale-out, and controller-profile diagnosis (`--readiness-impact`, `--rollout`, `--scaleout-blockers`, `--controller-profile`, `--decision-trace`).
- Added a `why-not-scale` subcommand to diagnose scaling blockers.
- Added an `advisor` command with a `container-resource` subcommand.
- Added `ownership`, `fleet`, `readiness`, and `profile detect` subcommands plus a `policy init` command.
- Added flapping diagnosis with replica-range analysis and HPA conflict detection.
- Added estimated scaling reasons, HPA health-trend tracking, and enhanced stabilization display.
- Added GitOps and policy workflows to HPA comparison and analytics.
- Added multiple candidate configurations, pod-hours, and capped-duration metrics to the replay lab.
- Added `--explain` and assumption override flags to the assumptions command, plus a `--startup-context` flag.
- Added a dedicated roadmap document, a `make docs-check` README synchronization check, and five-minute quick start sections to both READMEs.

### Changed
- Renamed diagnostic confidence labels to an observed/estimated model.
- Refactored lint rules to use `context`; formatted struct field alignments and removed unused types.
- Expanded documentation: roadmap updates, jUnit/SARIF and carbon-cost notes, TUI manual links, and an expanded workflow gallery.

### Fixed
- Fixed deferred `Close`/`Chdir` error handling and explicit write-result handling in bundle Markdown rendering.
- Fixed the Japanese README CI badge repository URL and synchronized README content.

## [0.9.0] - 2026-06-08

### Added
- Added a `lint` command and enabled advisor flags in `doctor`.
- Added warmup analysis for post-scale-out readiness.
- Added churn detection, metric hints, VPA advisory, and a history view.

## [0.8.0] - 2026-06-08

### Added
- Added GitOps and metric-contract checks plus profile-based recommendations.
- Added a `capacity-plan` command for diagnosing `maxReplicas` safety.
- Added a scale-out blockers command with deep capacity analysis.
- Added a `--scale-path` flag to explain the HPA scaling path.
- Added a metrics-freshness analyzer (`--metrics-freshness`) for metric staleness diagnosis.
- Added a `policy` command and interactive TUI simulation views.
- Added a retrospective scaling timeline (`timeline` subcommand and `--since` flag).

### Changed
- Refactored `runList` into focused helper functions.
- Documented the doctor subcommand, the before/after `--explain` comparison, and simplified Krew install instructions.
- Bumped the Kubernetes dependency group and `codecov/codecov-action` from v5 to v6.

## [0.7.0] - 2026-06-06

### Added
- Added a What-If Simulator, Multi-Metric Trace, and Best-Practice Auditor.
- Exposed KEDA/VPA enrichment skip reasons in debug output.

### Changed
- Unified diagnostics, improved health weights, and hardened apply safety with client reuse.
- Introduced typed health states and signals and made diagnostic confidence a structured field.
- Split `AnalyzeWithOptions` into analysis phases and extracted `statusOptions.Normalize()` and rule-based suggestions.
- Refactored the options struct into sub-structs and narrowed helper functions.

### Fixed
- Improved metric matching, health scoring, and diagnostic accuracy.
- Met the CI coverage threshold and resolved golangci-lint findings.

## [0.6.0] - 2026-06-04

### Added
- Added simulation, capacity-context, and replay commands.
- Added comprehensive codebase improvements across six phases.

### Changed
- Slimmed the README to ~165 lines, moving details into `docs/`.

### Fixed
- Resolved golangci-lint errors from CI.

## [0.5.0] - 2026-06-02

### Added
- Added auto-detection of KEDA ScaledObject and VPA conflict enrichment.
- Added KEDA/VPA health penalties and `scaleTargetRef` validation.

### Changed
- Configured release-notes generation and included docs in release notes.
- Updated contributing guidelines, architecture, and the Japanese README.

## [0.4.0] - 2026-06-02

### Added
- Added shell completion support.
- Added configuration file support and a documented example config.
- Added JSON output schema documentation.
- Added cluster-wide resource context and deeper KEDA/VPA diagnostics.
- Added autoscaler diagnostic reports.
- Added TUI and large-cluster usability improvements.
- Added score-based tier labels to health visualization.

### Changed
- Expanded README and architecture documentation for command coverage, JSON schema, Krew status, and supported Kubernetes versions.
- Expanded HPA diagnostics and large-cluster UX.
- Deepened KEDA analysis and autoscaler troubleshooting output.
- Resolved golangci-lint findings before release.

## [0.3.0] - 2026-06-01

### Added
- Added `--suggest`, `--fix`, and `--apply` workflows with structured patch suggestions.
- Added health scores, richer list output, metric bars, and compact behavior summaries.
- Added Japanese text labels through `--lang=ja` / `-o ja`.
- Added Makefile targets for build, test, coverage, lint, E2E, and release checks.
- Added Dependabot and CodeQL workflows.
- Added Renovate configuration, govulncheck, and gosec CI checks.
- Added GoReleaser SBOM and Homebrew Cask metadata for the dedicated tap.
- Added `scan` and `list --problem` for cluster-wide HPA problem triage.
- Added `list --health-score <threshold>` for filtering HPAs by low health score.
- Added reusable status, list, and watch asciinema demo sources plus a comparison visual.
- Added a larger SVG screenshot gallery covering explain, list, watch, suggest, dry-run apply, Japanese output, JSON, and common failure states.
- Added architecture, security, RBAC, and richer issue/PR documentation.
- Added `version` subcommand for build metadata.
- Added practical `examples/` manifests for CPU/memory, behavior, custom metrics, and KEDA-style HPA scenarios.

### Changed
- Upgraded Kubernetes client libraries to `k8s.io/*` v0.35.0.
- Expanded README badges, demo links, installation examples, and development documentation.
- Clarified Krew command naming, dry-run modes, and explicit HPA analysis limitations in README and Krew caveats.
- Made `--apply` dry-run by default, with patch diff output and explicit `--dry-run=false` required for persistence.
- Added commit and build date to release version metadata.
- Added safety preconditions and warnings to structured suggestions, and made copy-paste patch commands dry-run by default.
- Expanded E2E command coverage for Japanese output and cluster-wide `scan`.
- Upload coverage to Codecov from CI while keeping coverage upload non-blocking.
- Expanded Japanese README coverage to match the English usage, safety, CI/CD, validation, and known-gap sections.
- Hardened HPA analysis nil handling and moved health score penalties into named constants.

### Fixed
- Fixed GoReleaser Homebrew and SBOM configuration issues.
- Fixed security scanner findings and lint failures across CLI, tests, and CI configuration.
- Fixed E2E handling for HPAs with ERROR health status.

## [0.2.0] - 2026-05-30

### Added
- **Multi-HPA Watch Mode:** Added support for periodically watching all HPAs or multiple HPAs using `kubectl hpa status list --watch` or the `-w` shorthand.
- **Robust Color Table Rendering:** Handled ANSI escape character length dynamically with `lipgloss.Width` to fix column alignment issues in colored output.
- **Enhanced Non-Resource Metric Parsing:** Added ratio and note calculations for Pods, Object, and External metric sources using `resource.Quantity` ratios.
- **Sorting Enhancements:** Added support to sort by current-desired difference (`--sort-by=diff`) and resource age (`--sort-by=age`).
- **Comprehensive E2E integration test suite:** Added `test/e2e/e2e_test.go` running on a temporary local `kind` cluster context.
- **Phase 2 Edge-Case Unit Tests:** Covered 10% HPA tolerance boundaries, maxReplicas multi-metric winner cases, and custom stabilization windows.
- **CI/CD Workflow Improvements:** Added a automated `kind` cluster setup and E2E testing to the GitHub Actions workflow.

### Fixed
- Prioritized issues in `NewListItem` to bubble up `ERROR` and `LIMITED` conditions cleanly in list output.
- Escaped percent formatting in test assertions.

## [0.1.0] - 2026-05-24

### Added
- Initial proof-of-concept release.
- Interactive status analysis of HPA scaling parameters based on K8s API signals.
- Single HPA watch, list filters, and basic YAML/JSON format output support.
