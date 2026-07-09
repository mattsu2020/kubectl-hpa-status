# Roadmap

This roadmap tracks planned work that is visible to users and contributors. It is intentionally separate from the README so the README stays focused on installation and daily usage.

## Near Term

- **E2E scenario coverage:** Expand kind E2E coverage for multi-metric HPAs, KEDA-style external metrics, VPA conflict detection, and stabilization boundary cases. Behavior-policy visualization is covered by `TestE2E_BehaviorPolicies`.
- **README sync quality gate:** Keep `README.md` and `README.ja.md` structurally aligned through `make docs-check` and CI.

## Done in 2.0

- **Removed deprecated `analyze` command:** The `analyze` (alias `diagnose`) subcommand was removed. Use `status NAME --explain`.
- **Removed deprecated flag aliases:** `--recommend` (use `--suggest`), `--export-patch` (use `--export`), and the list flag `--max-score` (use `--health-score`) were removed.
- **Removed deprecated top-level `alpha` aliases:** Operational and experimental commands (`policy`, `gitops`, `bundle`, `incident-bundle`, `support-bundle`, `capacity`, `capacity-gap`, `autoscaler-map`, `analyze-record`, `flap`) now live exclusively under the `alpha` parent; the historical top-level paths were removed. Use `alpha <cmd>`.
- **`Analysis` additive grouping:** Added full read-only group-view methods for all 13 ROADMAP groups (`Meta`, `Replicas`, `Decision`, `MetricsGroup`, `ConditionsGroup`, `Capacity`, `ScaleToZeroGroup`, `Stability`, `Advisory`, `Controllers`, `Blockers`, `ActionsGroup`, `Lifecycle`) as step 1 of the v2 grouping. Also added `Analysis.HealthState()` for typed access while keeping `Health` as a JSON string. The flat fields and JSON shape are unchanged; a future major version will retire the flat fields.
- **Actions SSOT:** `RecommendedActions` and `buildStructuredActions` share `collectActionCases` so human and structured action lists cannot diverge on the core analyze path.
- **`cmd/` sub-package extraction (phase 1):** Lifted shared helpers into `cmd/internal/{errs,client,output}` and extracted the bundle renderer layer into `cmd/bundle`, following the facade-then-migrate pattern. Further groups (`replay`, `alerts`/`completion`/`compat`/`version`) remain in `cmd/`.
- **Status enricher phases:** `buildStatusEnrichers` is split into named dependency phases (`core` → `metricsPods` → `capacity` → `advisors`) with a pinned name order test.
- **`client.LookupHPA`:** The create-client + fetch-HPA helper lives in `cmd/internal/client` (cmd facade retained).
- **Error sentinel hygiene:** Added `ErrNoRecordedSnapshots`, `ErrPolicyViolations`, `ErrPolicyGuardBlocked`, and `ErrInvalidCandidateSpec` so exit paths are matchable via `errors.Is`.
- **Nil-safety:** Guarded `*deploy.Spec.Replicas` / `*sts.Spec.Replicas` dereferences in the GitOps conflict path.
- **Test coverage:** Lifted coverage across `cmd/` (12 previously-untested files), `internal/cmdoptions` (34.9% → 61.2%), `pkg/hpa/keda` (45.8% → 96.6%), and split the 1934-line `test/e2e/e2e_test.go` into per-area files.
- **Large test file splits:** Split `pkg/hpa/analysis_test.go` (~1900 lines) into domain files (`analysis_core`, `structured`, `metrics`, `health`, `suggestions`, `text`, `helpers`) and `cmd/root_integration_test.go` into status/list/watch/simulate integration files.
- **E2E behavior policies:** `TestE2E_BehaviorPolicies` asserts `behavior -o json` scaleUp/scaleDown policies and status --explain visibility.

## Medium Term

- **Informer-based watch:** Add an opt-in informer update path for large clusters alongside the current polling mode.
- **KEP-6111 upstream adapter:** Replace the current visible-signal structured export with native upstream structured HPA decision fields when they become available.

## Structural Refactors (Internal)

These are internal-only changes tracked separately because they touch wide
areas and require their own design step before landing. They have no
user-visible behavior change.

- **Split `cmd/` into sub-packages:** `cmd/` currently holds ~110 files in one
  `package cmd`. Extract self-contained groups (`bundle_*`, `replay`, then
  shallower commands like `alerts`/`completion`/`compat`/`version`) into
  sub-packages. Prerequisite: lift the ~10 unexported helpers they share
  (`newClientOrDefault`, `applyCommandPreset`, `fetchSnapshot*`,
  `capacitySelector`, `redactBytes`, `outputSelection`, `writeOutput`, ...) into
  a shared `cmd/internal` package first, then migrate callers and shrink the
  `cmd/converters.go` / `cmd/output.go` facades.
- **Slim the `Analysis` god-struct:** `pkg/hpa.Analysis` has 65 fields
  accumulated feature-by-feature. Plan a JSON-schema v2 grouping so related
  fields travel together. This is a breaking JSON change and must ride a major
  version bump with additive migration.

  **Proposed v2 grouping (work-in-progress, subject to design review):**

  | Group | Fields (current) | Notes |
  |---|---|---|
  | `Meta` | `Namespace`, `Name`, `Target`, `CreationTimestamp` | HPA identity; stable, top-level today |
  | `Replicas` | `Current`, `Desired`, `Min`, `Max`, `TargetReplicas` | Core scaling envelope |
  | `Decision` | `Health`, `HealthScore`, `HealthResult`, `DecisionTrace`, `MetricDecisionTrace`, `StructuredDecisionTrace`, `DecisionSignals`, `ImpactMetric`, `Summary`, `SummaryKey` | Why this replica count |
  | `Metrics` | `Metrics`, `MetricsDiagnostics`, `MetricFreshnessEntries`, `MetricContract`, `MetricHints`, `AdapterDiagnostics` | Metric pipeline health |
  | `Conditions` | `Conditions`, `Behavior`, `StabilizationWindowSeconds`, `StabilizationSource`, `StabilizationConfidence`, `StabilizationRemaining` | HPA controller conditions + behavior |
  | `Capacity` | `CapacityContext`, `CapacityHeadroom`, `CapacityPlan`, `ResourceCheck`, `PodAnalysis`, `ScalePath`, `ReadinessImpact` | Scheduling/capacity picture |
  | `ScaleToZero` | `ScaleToZero`, `WarmupAnalysis` | Scale-to-zero subsystem (shared cold-start theme) |
  | `Stability` | `FlappingSimulation`, `FlappingPrevention`, `FlappingDiagnosis`, `ChurnAnalysis` | Flapping/churn diagnosis |
  | `Advisory` | `VPAConflict`, `VPAAdvisory`, `ContainerAdvisor`, `BehaviorAdvisor` | VPA/container tuning advice |
  | `Controllers` | `KEDAInfo`, `RolloutDiagnosis`, `ControllerProfile` | External controller integrations |
  | `Blockers` | `BlockerReport`, `GitOpsConflict` | Apply-time gating |
  | `Actions` | `Actions`, `Suggestions`, `StructuredActions`, `StructuredInterpretation`, `Interpretation`, `Assumptions`, `Warnings` | Recommendations + explainability |
  | `Lifecycle` | `StaleStatus`, `HealthTrend`, `EnrichmentStatus`, `Debug`, `HiddenFactors` | Freshness/trend/telemetry |

  **Migration strategy (additive):**
  1. ~~Introduce nested structs / group views alongside the flat fields.~~
     **Done (read-only group views for all 13 groups + `HealthState()`).**
     Nested *storage* (fields on `Analysis` itself) remains a later step.
  2. Add accessors that read from nested storage when present, falling back
     to the flat field — keeps internal callers working during migration.
     Group-view methods already provide the read path over flat storage.
  3. Emit JSON with both flat (v1) and nested (v2) keys for one minor release,
     behind `--output-schema v2`.
  4. Flip the default and drop the flat keys at a future major bump.

  Step 1 (views) is landed; step 2 can proceed incrementally; step 3+4 are
  the breaking boundary. The grouping above mirrors the existing
  `pkg/hpa/{keda,vpa,blocker,warmup,flapping,churn,policy,lint,readiness}`
  sub-package boundaries so each group maps to one owning sub-package.
- **Re-evaluate testutil SA1019 suppressions:** `internal/testutil` uses
  `fake.NewSimpleClientset` (deprecated, no applyconfig replacement). Re-check
  on each client-go upgrade and remove the `//nolint:staticcheck` once an
  alternative lands.

## Recently Added

- **Durable decision recording:** `record` writes JSONL HPA snapshots and `timeline --from-record` replays them after Events expire.
- **Preflight and impact commands:** `preflight`, `behavior`, and `estimate` cover capacity validation, behavior visualization, and rough cost impact.
- **Metrics adapter probe:** `metrics probe` combines freshness, contract, adapter diagnostics, and metric hints for custom/external metrics.
- **CI/report outputs:** `lint -o github` emits GitHub Actions annotations and `scan --summary --report markdown|html` produces cluster summary reports.
- **GitOps and policy workflows:** `--export-patch`, `recommend --policy`, and `compare -A --only-drift` support PR-based operations and environment drift review.
- **Operationalization:** `alerts generate` creates starter monitoring rules and `analyze-record --detect flapping` turns durable records into churn insights.
- **Explainability and TUI safety:** `--format structured`, `explain`, score breakdowns, hidden decision factors, and in-TUI two-step batch apply preview improve operator confidence.
- **Trend and tuning workflows:** `history`, `tune`, `slo`, Prometheus query links, and carbon-aware `estimate` connect HPA behavior to incidents, SLOs, cost, and sustainability.
- **CI/CD and GitOps reporting:** `scan/list --report junit|sarif`, `list --gitops-drift`, `export --prometheus`, and local AI context packs make HPA health easier to automate and share.

## Release and Supply Chain

**Done (wired in `.goreleaser.yml` and `.github/workflows/release.yml`):**

- Cosign keyless signing of release archives and checksums (sigstore OIDC).
- SLSA build provenance via `actions/attest-build-provenance`.
- SBOM emission for release archives in GoReleaser.

**Ongoing:**

- Use pre-releases for experimental workflows and reserve stable releases for validated user-facing behavior.
- Document verification steps in `SECURITY.md` when release packaging changes.

## Community

- Label small, verifiable issues with `good first issue`.
- Keep contribution scopes explicit: target file or command, expected behavior, and validation command.
- Publish release highlights with user-facing changes, risks, and upgrade impact rather than commit hashes only.
