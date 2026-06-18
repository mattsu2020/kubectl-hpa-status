# Architecture

`kubectl-hpa-status` is a kubectl plugin that turns visible
`autoscaling/v2` HorizontalPodAutoscaler signals into operator-focused status,
health, and safe next-action suggestions.

## Boundaries

The tool intentionally does not reimplement the HPA controller. It reads only
stable Kubernetes API surfaces:

- HPA spec and status
- HPA conditions
- current metric status
- `spec.behavior`
- recent Events
- HPA labels and annotations used to detect generated or KEDA-managed HPAs

When Kubernetes does not expose an internal decision, the output must say so.
Inference should be labeled with confidence language and covered by tests.

## Package Layout

| Path | Responsibility |
| --- | --- |
| `cmd/` | Cobra commands, flags, Kubernetes client orchestration, output format routing (~70 files, one feature/subcommand per file) |
| `pkg/hpa/` | Importable analysis model: HPA signal extraction, health scoring, suggestions, diagnostics, and text/Markdown/HTML/SARIF rendering |
| `internal/kube/` | kubeconfig resolution, client construction, KEDA/VPA/node/quota reads, scale-target and pod info, test helpers |
| `internal/enrichment/` | Batched KEDA/VPA enrichment context and status tracking |
| `internal/tui/` | Bubble Tea dashboard: model/update/view plus a per-view renderer |
| `internal/history/` | Health snapshot store for trend/sparkline replay |
| `internal/i18n/` | Embedded locale bundles (en/ja), dynamically loaded from `locales/` |
| `pkg/style/` | Terminal color and semantic styling (shared by cmd and pkg/hpa renderers) |
| `internal/patch/` | Strategic merge patch helpers for suggestions |
| `test/e2e/` | kind-backed command path tests |

`pkg/hpa/` files follow a per-domain suffix convention: `analysis.go`
+ `analysis_phases.go` drive `Analyze`/`AnalyzeWithOptions`; each domain
(`audit`, `capacity`, `gitops`, `simulate`, `health`, `metric`, `blocker`,
`retrospective`, `warmup`, ...) is split across `_types` (data), `_rules`
(detection logic), and `_text` (rendering) files. `clock.go` injects `now`
for deterministic rendering; `report.go` emits Markdown/HTML incident reports.

### cmd/ file responsibilities

Each subcommand is one file exposing a `newXxxCommand(opts *options)`
constructor. Major commands grouped by area:

| Area | Commands |
| --- | --- |
| Status & diagnosis | `status`, `explain`, `doctor`, `analyze`, `assumptions`, `why_not_scale`, `readiness`, `readiness_doctor` |
| Cluster overview | `list`, `scan`, `fleet`, `watch`, `tui`, `compare` |
| Deep analysis | `timeline`, `trace`, `path`, `replay`, `record`, `simulate*`, `metrics_probe`, `metrics_contract` |
| Recommendations | `recommend`, `advisor`, `container_advisor`, `capacity*`, `profile`, `tune`, `slo` |
| Lint & policy | `lint`, `policy`, `gitops_lint`, `gitops_review`, `blockers` |
| Bundles & export | `bundle*`, `incident_bundle`, `support_bundle`, `snapshot`, `export*` |
| Plumbing | `root`, `output`, `config`, `helpers`, `exitcode`, `completion` |

Refactoring notes:
- `status.go` was split into per-enrichment helpers (`enrichXxx` functions
  extracted from `buildStatusReport`); KEDA/VPA data fetching still lives in
  `internal/kube/`.
- `output.go` handles format routing; config loading lives in `config.go` /
  `config_apply.go`.
- HPA fetch (`kube.GetHPAFromClient`) and event conversion
  (`hpaanalysis.EventsFromCore`) are centralized in `internal/kube/hpa.go` and
  `pkg/hpa/events.go` respectively; commands call them instead of inlining the
  raw client calls.
- Client creation goes through `newClientOrDefault` (`cmd/client_helpers.go`)
  so the standard "failed to create Kubernetes client" message is shared.
- `EnrichmentStatus` on `Analysis` is now a typed `*hpaanalysis.EnrichmentStatus`
  (mirror of `internal/enrichment.Status` via `Status.ToAnalysisStatus`) instead
  of `interface{}`.
- Config-file accepted values for color/output/lang are defined once in
  `config.go` (`validColorValues` / `validOutputValues` / `validLangValues`)
  and reused by both `validateConfig` and the flag descriptions in
  `root_flags.go`.
- The text orchestrators (`WriteStatusTextWithOptions`, `WriteStatusDiff`,
  `WriteHTMLReport`) delegate to per-section renderers (`text_extras.go`,
  `diff_text_sections.go`, `report_html_sections.go`) so no `//nolint:gocyclo`
  exemption is needed on the orchestrator body.
- The `options` struct in `root.go` is shared across all commands. Per-command
  option splits and `cmd/` sub-packages are deferred: shared types/helpers
  create import-cycle risk, so prefer adding fields over splitting until a
  dedicated interface boundary is designed. When that boundary lands, extract
  one sub-package at a time (start with the most self-contained group, e.g.
  `bundle_*`) and re-export the shared symbols through a thin facade to keep
  the rest of `cmd/` compiling.
- Two cobra-free layers have already been extracted out of `cmd/`:
  `internal/kubeconv` (kube.* -> pkg/hpa DTO conversion, with `cmd/converters.go`
  as a thin facade) and `internal/render` (output format routing and
  serialization, with `cmd/output.go` as a thin facade). When the `cmd/` split
  lands, callers migrate to `kubeconv.*` / `render.*` directly and the facades
  shrink.
- `pkg/hpa/render` extraction of the report renderers is complete: the
  Markdown/HTML/list/incident report files (`report_markdown.go`,
  `report_html.go`, `report_html_sections.go`, `report_list.go`,
  `report_incident.go`) now live in `pkg/hpa/render`, and the shared
  HTML/Markdown escape helpers (`escapeMarkdown`, `htmlEscape`,
  `htmlHealthBadge`, `htmlCSS`) live in `pkg/hpa/rendutil` to break the
  import cycle (both `pkg/hpa` and `pkg/hpa/render` need them). The remaining
  `*_text.go` files (status text, diff text, advisor text) are still in
  `pkg/hpa` because they share the `FormatMetricStatus`/`labels` machinery
  with the analysis core; moving them requires injecting those call sites
  through an interface first.
- `cmd/options_bridge.go` is the single vocabulary for `internal/cmdoptions`
  symbols inside `cmd/`. Every preset const, type alias
  (`options`, `commonOptions`, `commandPresetOptions`, ...), and helper
  (`applyCommandPreset`, `defaultRootOptions`, `validAnalysisProfiles`) lives
  there; command files must NOT import `internal/cmdoptions` directly. Add new
  presets/types to the bridge rather than reaching into the package.
- `Analysis.Warnings` (`[]string`) records enrichment-pipeline failures and
  notable skip reasons. They are rendered in plain-text status output (via
  `appendWarningsSection`) as well as JSON/YAML, so a transient fetch failure
  or RBAC denial is visible to operators instead of silently degrading to an
  empty sub-report. New enrichment steps should append to `Warnings` on
  best-effort failure rather than swallowing the error.

`pkg/hpa` is kept importable so downstream tools can reuse the analysis model
without depending on Cobra command wiring.

## Analysis Flow

1. `cmd` loads one or more HPAs through the Kubernetes client.
2. `pkg/hpa.Analyze` converts raw HPA objects into a structured `Analysis`.
3. Conditions, metrics, behavior, health, interpretation, and suggestions are
   attached to the same model.
4. Output writers render text, JSON, YAML, JSONPath, or templates.

## Health Score Algorithm

The health score starts at 100 and deducts configurable penalties for each detected condition:

| Condition | Default Penalty | Health State | Description |
|-----------|----------------|--------------|-------------|
| `ScalingActive != True` | -45 | ERROR | Metrics not available; HPA cannot compute recommendations |
| `AbleToScale != True` | -35 | ERROR | HPA controller unable to scale (config or permission issue) |
| `ScalingLimited == True` | -25 | LIMITED | HPA capped by minReplicas or maxReplicas |
| Implicit maxReplicas (current==desired==max) | -20 | LIMITED | Desired replicas equal maxReplicas without explicit ScalingLimited |
| `ScaleDownStabilized` | -10 | STABILIZED | Scale-down suppressed by stabilization window |
| At minimum replicas | -5 | (no change) | Desired replicas equal minReplicas |
| KEDA inactive trigger | -15 | LIMITED | External event source not producing events; scale-up may not trigger |
| VPA conflict | -20 | LIMITED | Both VPA and HPA target the same resource on the same workload |

Health states (in priority order): `ERROR` > `LIMITED` > `STABILIZED` > `OK`.

Score is clamped to [0, 100]. All penalties are configurable via repeated
`--health-weight name=value` flags or config file.

The default weights favor operator urgency over mathematical precision:

- Metrics unavailability gets the largest deduction because the HPA cannot
  compute a trustworthy recommendation.
- `AbleToScale != True` is nearly as severe because the controller is reporting
  it cannot act on the desired scale.
- min/max limiting is lower because it can be intentional capacity policy, but
  it still requires attention when demand remains high.
- implicit maxReplicas receives a smaller penalty than explicit
  `ScalingLimited` because it is inferred from replica counts and may be a
  transient status lag.
- stabilization and at-minimum deductions are advisory. They explain surprising
  no-op behavior without turning an otherwise healthy HPA into an error.

### Example scores:
- Healthy HPA at steady state: 95 (-5 for at-minimum)
- HPA at maxReplicas: 75 (-25 ScalingLimited) or 80 (-20 implicit)
- Metrics unavailable: 55 (-45 ScalingInactive, -10 at-minimum) → ERROR

## CLI Defaults And Config

Runtime defaults can come from flags or an optional config file. The default
config path is `~/.kube/hpa-status.yaml`; `--config` selects another file.
Config values are applied only when the corresponding CLI flag was not set.
This keeps command-line invocations deterministic while allowing teams to set
defaults for namespace, language, color, event limits, score filters, and
health-score weights.

## Watch Dashboard

`--watch` remains a simple polling loop over Kubernetes API reads.
`--dashboard` opens the interactive Bubble Tea dashboard when stdout is an
interactive terminal. Non-interactive stdout keeps the compact output-only
dashboard for scripts and recordings.

The `tui` subcommand is the interactive Bubble Tea path. It reuses the same
`Analysis` and `ListItem` models, supports refresh/pause/filter/detail
navigation, accepts the same refresh interval, and paginates Kubernetes list
calls. Keep JSON/YAML output unchanged when expanding the TUI.

## KEDA And Adapter Context

KEDA and custom/external metrics adapter support is currently limited to
signals visible on the HPA itself. The analyzer detects KEDA-style labels,
annotations, and `keda-hpa-*` names, then points operators to ScaledObject and
adapter diagnostics. Direct reads of KEDA CRDs should be added through a
separate optional client path so clusters without KEDA do not pay that cost.
External, object, and pods metrics include selectors in the formatted metric
model when they are visible in HPA status/spec, but adapter query internals such
as PromQL are still outside the HPA API surface.

Karpenter and Cluster Autoscaler integration should follow the same rule:
surface relationships that are explicit in Kubernetes objects first, then add
optional reads of provider-specific CRDs or logs only behind flags. The HPA
analysis model should say "scaling is capped or pending"; autoscaler adapters
can add "new nodes are pending" or "node provisioning is blocked" context
without changing the HPA decision summary.

## KEDA Detection Design

KEDA detection uses a two-layer model to identify KEDA-managed HPAs:

**Layer 1: Heuristic detection** (`pkg/hpa/interpret.go` `looksLikeKEDAManaged()`)

This layer inspects only the HPA object itself using three signals:

1. Label keys or values containing `keda.sh` or `keda` (case-insensitive)
2. Annotation keys or values containing `keda.sh` or `keda`
3. HPA name prefixed with `keda-hpa-`

This heuristic is fast and requires no additional API calls, but it can produce
false positives (an HPA named `keda-hpa-*` that is not KEDA-managed) and false
negatives (a KEDA-managed HPA with a custom name and no KEDA labels/annotations).
It is used for informational diagnostics and low-risk suggestions only.

**Layer 2: CRD-based detection** (`internal/kube/keda.go` `DetectKEDA()` and `FindScaledObjectForHPA()`)

This layer performs real ScaledObject CRD lookups through the Kubernetes dynamic
client. It uses the `--keda` flag to opt into CRD-based enrichment, which fetches
the ScaledObject and extracts trigger status, health conditions, fallback config,
and scaling policies. Clusters without KEDA installed do not pay this cost.

**When to use `--keda` flag:**

- To confirm whether an HPA is genuinely KEDA-managed
- To retrieve ScaledObject trigger health status (Active/Inactive/Unknown)
- To diagnose external metric issues rooted in scaler misconfiguration
- To check for KEDA inactive trigger penalties in health scoring

**Future improvement path:**

- Add an opt-in direct CRD fetch that bypasses the heuristic entirely
- Cache ScaledObject lookups to reduce API server load during watch/list
- Support KEDA v1alpha2 API version alongside v1alpha1

## Large Cluster Lists

`list`, `scan`, and `tui` use Kubernetes ListOptions pagination by default.
The default page size is 500 and is configurable with `--chunk-size` or
`chunkSize` in config. Keep per-item analysis streaming-friendly: avoid retaining
raw HPA objects after converting them to `ListItem`/`StatusReport`, and prefer
selector filtering at the API server via `--selector` when possible.

`list --apply` and `scan --apply` reuse the same dry-run-first suggestion
workflow as single-HPA status, but require an explicit bounded selection such as
`--problem`, `--filter`, or score filters. This prevents accidentally applying
suggestions to every HPA returned by an unbounded cluster-wide list.

## Suggestion Safety

Patch suggestions are conservative:

- Suggestions require visible HPA status evidence.
- Copy-paste commands include `--dry-run=server`.
- `--apply` defaults to server-side dry-run.
- Persistent changes require `--dry-run=false`.
- maxReplicas suggestions include preconditions and capacity warnings.

## Future Design Notes

Kubernetes may eventually expose structured HPA scaling decisions. If that API
arrives, add it as another input signal rather than replacing the existing
analysis model. The current boundary should remain: use explicit controller
signals when available, and clearly label inference when they are not.

As of 2026-06-01, kubernetes/kubernetes#138992 is closed in favor of
kubernetes/enhancements#6107 and kubernetes/enhancements#6111. Those follow-up
items are also closed, and no generated Kubernetes client type for structured
HPA decision status is available in this repository yet. Keep this integration
as a prepared boundary, not an active API dependency.

Concrete integration plan:

- Add a small adapter that converts the new structured decision fields into the
  existing `Analysis` model instead of leaking raw API shape into renderers.
- Keep that adapter behind an interface such as `DecisionSignals` so tests can
  exercise future Kubernetes fields before the generated client types are
  widely available.
- Prefer structured controller reasons over current best-effort inference for
  metric winner, tolerance, missing-metric handling, and stabilization history.
- Keep the old inference path as a fallback for older clusters and mark it with
  lower confidence when structured decisions are absent.
- Extend JSON/YAML output with additive fields only; do not rename existing
  fields such as `summary`, `conditions`, `metrics`, or `suggestions`.
- Add fixture tests that compare the same HPA with and without structured
  decision data so behavior remains compatible across Kubernetes versions.
