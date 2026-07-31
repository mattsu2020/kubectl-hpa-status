# Roadmap

This roadmap tracks planned work that is visible to users and contributors. It is intentionally separate from the README so the README stays focused on installation and daily usage.

## Near Term

- **E2E scenario coverage:** Expand kind E2E coverage for multi-metric HPAs, KEDA-style external metrics, VPA conflict detection, and stabilization boundary cases. Behavior-policy visualization is covered by `TestE2E_BehaviorPolicies`.
- **README sync quality gate:** Keep `README.md` and `README.ja.md` structurally aligned through `make docs-check` and CI.

## Done in Unreleased 2.x

- **Removed deprecated `analyze` command:** The `analyze` (alias `diagnose`) subcommand was removed. Use `status NAME --explain`.
- **Removed deprecated flag aliases:** `--recommend` (use `--suggest`), `--export-patch` (use `--export`), and the list flag `--max-score` (use `--health-score`) were removed.
- **Removed deprecated top-level `alpha` aliases:** Operational and experimental commands (`policy`, `gitops`, `bundle`, `incident-bundle`, `support-bundle`, `capacity`, `capacity-gap`, `autoscaler-map`, `analyze-record`, `flap`) now live exclusively under the `alpha` parent; the historical top-level paths were removed. Use `alpha <cmd>`.
- **Versioned status schema:** Added all 13 read-only `Analysis` group views and the opt-in `--output-schema=v2` projection for status JSON, YAML, JSONL, JSONPath, and Go templates. v1 remains the default compatible flat contract; v2 has its own checked-in JSON schema and preserves multi-HPA item errors.
- **Actions SSOT:** `RecommendedActions` and `buildStructuredActions` share `collectActionCases` so human and structured action lists cannot diverge on the core analyze path.
- **`cmd/` sub-package extraction (phase 1):** Lifted shared helpers into `cmd/internal/{errs,client,output}` and extracted the bundle renderer layer into `cmd/bundle`, following the facade-then-migrate pattern. Further groups (`replay`, `alerts`/`completion`/`compat`/`version`) remain in `cmd/`.
- **Status enricher phases:** `buildStatusEnrichers` is split into named dependency phases (`core` → `metricsPods` → `capacity` → `advisors`) with a pinned name order test.
- **Shared analysis and observation boundaries:** Added `internal/analysis` for list/TUI finalization and `internal/observation` for request-scoped, memoized scale-target/Pod reads with typed availability states.
- **Shared history service:** Status and list now use one clock-injected recorder for append, retention pruning, load, and health-trend analysis.
- **Canonical metric identity:** Spec/status correlation and simulation identify metrics by source, name, container, canonical selector, and described object. Ambiguous name-only overrides fail closed instead of selecting the first metric.
- **Typed capacity and retrospective decisions:** Capacity rule checks use stable IDs/status enums instead of display-text matching, malformed quantities remain explicitly unknown, and retrospective rescale entries retain parsed replica ranges (including scale-from-zero).
- **Unified enrichment:** The generic pipeline engine lives in `internal/enrichment`; internal status types alias the canonical public model, and health penalties upsert their signals idempotently.
- **Immutable command requests:** Status/list/scan snapshot mutable Cobra options into deep-copied request DTOs before execution.
- **Direct rendering and conversion boundaries:** Command call sites now import `internal/render` and `internal/kubeconv` directly. The obsolete `cmd/converters.go` and render forwarding functions were removed, and text write errors are propagated.
- **Compatibility-facade gate:** `make facade-check` and CI reject new in-tree uses of deprecated public facades; ARCHITECTURE.md records the v3 removal criteria.
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
  sub-packages. Conversion and rendering callers are already on
  `internal/kubeconv` / `internal/render`, and immutable request DTOs now bound
  option mutation. The remaining prerequisite is to narrow command-only
  helpers such as snapshot loading, capacity selectors, output selection, and
  completion callbacks into explicit package contracts.
- **Slim the `Analysis` god-struct:** `pkg/hpa.Analysis` has 65 fields
  accumulated feature-by-feature. The additive migration boundary is now
  complete: v1 keeps the flat storage and default wire shape, while explicit
  v2 output uses 13 nested group views. Remaining work is a v3 design decision:
  make grouped values primary in-memory storage, flip the default only with
  migration notes, then retire the flat v1 fields in that major release.
- **Re-evaluate testutil SA1019 suppressions:** `internal/testutil` uses
  `fake.NewSimpleClientset` (deprecated, no applyconfig replacement). Re-check
  on each client-go upgrade and remove the `//nolint:staticcheck` once an
  alternative lands.
- **Extract `pkg/hpa/core` shared helpers:** `FormatMetricStatus`, the labels
  machinery, `TimelineSnapshot` helpers, clock, and conditions utilities are
  the shared dependency that keeps `capacity`, `simulate`, `decision`,
  `metrics`, `health`, `retrospective`, and `timeline` in the `pkg/hpa` root
  (see ARCHITECTURE.md "leaf domain extraction prerequisite"). Lifting them
  into `pkg/hpa/core` unblocks the domain extractions below.
- **Extract `simulate` and `capacity` domains:** Once `core` exists, move the
  tightly-coupled `simulate*.go` files (simulate, simulate_metric,
  simulate_extended, simulate_projection) into `pkg/hpa/simulate/`, and the
  `CapacityContext`/`CapacityHeadroom`/`CapacityPlan` trio into
  `pkg/hpa/capacity/` (with `blocker.nodeCapacityRule` re-homed to capacity),
  keeping deprecated re-export facades in `pkg/hpa` until the facade-removal
  policy below clears them.
- **TUI sub-models per view mode:** The first delegation boundary is in place:
  a mode-to-controller registry now owns rendering, view-local cursor movement,
  and Enter/Escape handling, with exhaustive registration tests and a safe
  fallback for unknown modes. `internal/tui.Model` still permanently holds six
  independent state machines. Move those states behind dedicated sub-models
  and narrow the remaining global key handling so adding a view becomes an
  isolated change.
- **Consolidate advisor/doctor command surfaces (user-visible):** `advisor`,
  `recommend`, and `container-advisor` overlap, as do `doctor`,
  `readiness-doctor`, and the `diagnosis-*` family. Plan subcommand grouping
  (`advisor container|behavior|recommend`, `doctor readiness|rollout|capacity`)
  with deprecated aliases for one minor release before removal.
- **Lower command-addition cost:** Adding one command today touches ~9 places
  (command file, commandGroups, options_bridge preset, cmdoptions preset +
  feature flag, enricher phase, Analysis field, text renderer, schema test).
  Evaluate an `AnalysisPlugin`-style registry (name + enrich + render) so a
  new analysis domain registers in one place.

## Recently Added

- **Durable decision recording:** `record` writes JSONL HPA snapshots and `timeline --from-record` replays them after Events expire.
- **Preflight and impact commands:** `preflight`, `behavior`, and `estimate` cover capacity validation, behavior visualization, and rough cost impact.
- **Metrics adapter probe:** `metrics probe` combines freshness, contract, adapter diagnostics, and metric hints for custom/external metrics.
- **CI/report outputs:** `lint -o github` emits GitHub Actions annotations and `scan --summary --report markdown|html` produces cluster summary reports.
- **GitOps and policy workflows:** `--export`, `recommend --policy`, and `compare -A --only-drift` support PR-based operations and environment drift review.
- **Operationalization:** `alerts generate` creates starter monitoring rules and `alpha analyze-record --detect flapping` turns durable records into churn insights.
- **Seasonality detection:** `alpha analyze-record --detect seasonality` finds recurring daily demand ramps in durable records and proposes scheduled pre-scaling (cron + KEDA cron trigger), addressing the HPA's reactive-only scale-up delay. Weekly cycles and metric-value-based (rather than replica-based) detection remain open; the latter needs a typed numeric metric field on `TimelineSnapshot`, which is currently a formatted string.
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
