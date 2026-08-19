# Roadmap

This roadmap tracks planned work that is visible to users and contributors. It is intentionally separate from the README so the README stays focused on installation and daily usage.

## Near Term

- **E2E scenario coverage:** Expand kind E2E coverage for multi-metric HPAs, KEDA-style external metrics, VPA conflict detection, and stabilization boundary cases. Behavior-policy visualization is covered by `TestE2E_BehaviorPolicies`.
- **README sync quality gate:** Keep `README.md` and `README.ja.md` structurally aligned through `make docs-check` and CI.
- **v4 preparation on the v3.x line:** Land the grouped-primary in-memory `Analysis` storage flip and additive record fields (typed numeric metric values on `TimelineSnapshot`) so that v4.0.0 only has to delete. See "v4 Breaking Changes (Planned)" below.

## Done through 3.0.0

- **Removed deprecated `analyze` command:** The `analyze` (alias `diagnose`) subcommand was removed. Use `status NAME --explain`.
- **Removed deprecated flag aliases:** `--recommend` (use `--suggest`), `--export-patch` (use `--export`), and the list flag `--max-score` (use `--health-score`) were removed.
- **Removed deprecated top-level `alpha` aliases:** Operational and experimental commands (`policy`, `gitops`, `bundle`, `incident-bundle`, `support-bundle`, `capacity`, `capacity-gap`, `autoscaler-map`, `analyze-record`, `flap`) now live exclusively under the `alpha` parent; the historical top-level paths were removed. Use `alpha <cmd>`.
- **Versioned status schema:** Added all 13 read-only `Analysis` group views and the opt-in `--output-schema=v2` projection for status JSON, YAML, JSONL, JSONPath, and Go templates. through v2, v1 remained the default compatible flat contract; v3 flipped the default to v2 (see below). v2 has its own checked-in JSON schema and preserves multi-HPA item errors.
- **Actions SSOT:** `RecommendedActions` and `buildStructuredActions` share `collectActionCases` so human and structured action lists cannot diverge on the core analyze path.
- **`cmd/` sub-package extraction:** Lifted shared helpers into
  `cmd/internal/{errs,client,output}`, shallow command domains into
  `cmd/internal/{alerts,buildinfo,compat,completion}`, bundle presentation into
  `cmd/bundle`, replay presentation into `cmd/replaylab`, and the three
  timeline-record JSONL readers into `cmd/internal/recordio`. Kubernetes/Cobra
  orchestration remains in `cmd` by design so extracted packages stay
  option-free and do not require a broad command facade.
- **Status enricher phases:** `buildStatusEnrichers` is split into named dependency phases (`core` → `metricsPods` → `capacity` → `advisors`) with a pinned name order test.
- **Shared analysis and observation boundaries:** Added `internal/analysis` for list/TUI finalization and `internal/observation` for request-scoped, memoized scale-target/Pod reads with typed availability states.
- **Shared history service:** Status and list now use one clock-injected recorder for append, retention pruning, load, and health-trend analysis.
- **Canonical metric identity:** Spec/status correlation and simulation identify metrics by source, name, container, canonical selector, and described object. Ambiguous name-only overrides fail closed instead of selecting the first metric.
- **Typed capacity and retrospective decisions:** Capacity rule checks use stable IDs/status enums instead of display-text matching, malformed quantities remain explicitly unknown, and retrospective rescale entries retain parsed replica ranges (including scale-from-zero).
- **Unified enrichment:** The generic pipeline engine lives in `internal/enrichment`; internal status types alias the canonical public model, and health penalties upsert their signals idempotently.
- **Immutable command requests:** Status/list/scan snapshot mutable Cobra options into deep-copied request DTOs before execution.
- **Direct rendering and conversion boundaries:** Command call sites now import `internal/render` and `internal/kubeconv` directly. The obsolete `cmd/converters.go` and render forwarding functions were removed, and text write errors are propagated.
- **HPA formatting core:** Localized labels and metric status/target/selector
  formatting live in `pkg/hpa/core`; the root package keeps compatibility
  aliases and forwarding functions for the v2 public API.
- **TUI view sub-models:** Six independent interactive state machines own
  their cloning and view-local transitions behind the mode controller
  registry, leaving the top-level model responsible for shared application
  state and global keys.
- **Compatibility-facade gate:** `make facade-check` and CI reject new in-tree uses of deprecated public facades; ARCHITECTURE.md records the v3 removal criteria.
- **`client.LookupHPA`:** The create-client + fetch-HPA helper lives in `cmd/internal/client` (cmd facade retained).
- **Error sentinel hygiene:** Added `ErrNoRecordedSnapshots`, `ErrPolicyViolations`, `ErrPolicyGuardBlocked`, and `ErrInvalidCandidateSpec` so exit paths are matchable via `errors.Is`.
- **Nil-safety:** Guarded `*deploy.Spec.Replicas` / `*sts.Spec.Replicas` dereferences in the GitOps conflict path.
- **Test coverage:** Lifted coverage across `cmd/` (12 previously-untested
  files), `cmd/internal/completion` (26.1% → 95.7%), `internal/cmdoptions`
  (34.9% → 61.2%), and `pkg/hpa/keda` (45.8% → 96.6%). Direct client-go
  `fake.NewSimpleClientset` calls are centralized in `internal/testutil`, so
  the temporary SA1019 suppression has one audited location. The 1934-line
  `test/e2e/e2e_test.go` was also split into per-area files.
- **Large test file splits:** Split `pkg/hpa/analysis_test.go` (~1900 lines) into domain files (`analysis_core`, `structured`, `metrics`, `health`, `suggestions`, `text`, `helpers`) and `cmd/root_integration_test.go` into status/list/watch/simulate integration files.
- **E2E behavior policies:** `TestE2E_BehaviorPolicies` asserts `behavior -o json` scaleUp/scaleDown policies and status --explain visibility.

## Medium Term

- **Informer-based watch:** Add an opt-in informer update path for large clusters alongside the current polling mode.
- **KEP-6111 upstream adapter:** Replace the current visible-signal structured export with native upstream structured HPA decision fields when they become available.

## Structural Refactors (Internal)

These are internal-only changes tracked separately because they touch wide
areas and require their own design step before landing. They have no
user-visible behavior change.

- **Split `cmd/` into sub-packages:** the flat `package cmd` still holds 96
  non-test files / ~14.6k lines. Phase 1 lifted shared helpers into
  `cmd/internal/{errs,client,output}` and the bundle renderers into
  `cmd/bundle`. Phase 2 extracted three of the four shallow command groups
  named here — `cmd/internal/compat` (report model, rules, text renderer),
  `cmd/internal/alerts` (Prometheus/Datadog rule templates), and
  `cmd/internal/buildinfo` (ldflags/build-info version resolution). Each is
  cobra-free and option-free, and each carries its own tests.

  `completion` is now extracted as `cmd/internal/completion`. The dynamic
  completers (HPA names, namespaces, contexts) take a narrow `completion.Deps`
  value (client factory + `AllNamespaces` + `Kubeconfig`) instead of closing
  over `*options`, and the value vocabularies (`output`, `color`, `lang`, ...)
  live as pure data in the package. `cmd/completion.go` is a thin facade that
  bridges `*options` → `completion.Deps` so the ~40 `hpaNameCompletion(opts)`
  call sites compile unchanged. The deeper commands (snapshot loading,
  capacity selectors, output selection) still reach cmd-private helpers and
  remain in `cmd/` as orchestration boundaries. Timeline JSONL scanning is
  centralized in `cmd/internal/recordio`; filtering, merging, snapshot limits,
  and legacy single-JSON fallback remain explicit at each command boundary.
- **Slim the `Analysis` god-struct:** `pkg/hpa.Analysis` has 65 fields
  accumulated feature-by-feature. The additive migration boundary is complete:
  through v2 the flat storage kept the default wire shape while explicit v2
  output used 13 nested group views; 3.0.0 flipped the default wire schema to
  v2. Remaining work is sequenced under the v4 plan: grouped values become the
  primary in-memory storage on the v3.x line (no wire change), and the flat v1
  fields are retired in v4.0.0.
- **Re-evaluate testutil SA1019 suppression:** All fake-client construction is
  routed through `internal/testutil`; its single `fake.NewSimpleClientset`
  call remains deprecated with no applyconfig replacement. Re-check on each
  client-go upgrade and remove the one `//nolint:staticcheck` once an
  alternative lands.
- **Extract `simulate` and `capacity` domains:** With the formatting/labels
  core now available, move the
  tightly-coupled `simulate*.go` files (simulate, simulate_metric,
  simulate_extended, simulate_projection) into `pkg/hpa/simulate/`, and the
  `CapacityContext`/`CapacityHeadroom`/`CapacityPlan` trio into
  `pkg/hpa/capacity/` (with `blocker.nodeCapacityRule` re-homed to capacity),
  keeping deprecated re-export facades in `pkg/hpa` until the facade-removal
  policy below clears them.
- **Consolidate advisor/doctor command surfaces (user-visible):** `advisor`,
  `recommend`, and `container-advisor` overlap, as do `doctor`,
  `readiness-doctor`, and the `diagnosis-*` family. Plan subcommand grouping
  (`advisor container|behavior|recommend`, `doctor readiness|rollout|capacity`)
  with deprecated aliases for one minor release before removal. See
  "v3 CLI surface consolidation" below for the decided scope.
- **Lower command-addition cost (evaluated):** Command construction and shared
  flag capabilities now live in one `commandSpec` registration, with registry
  consistency tests. A dynamic `AnalysisPlugin` registry is deferred because
  the typed `Analysis` payload and JSON schemas still require explicit changes;
  hiding those behind `name + enrich + render` would trade compile-time checks
  for runtime failures. The required v3 extension boundary and migration
  conditions are recorded in `docs/analysis-plugin-registry.md`.

## v3 Breaking Changes (Decided; CLI + facades + wire default executed)

This section is the single list of what the v3 major release removes. The CLI
surface consolidation, the deprecated-facade removal, and the default wire
schema flip are done on the v3 line; the remaining open item is the in-memory
`Analysis` storage flip below. Each executed item keeps one migration note in
`CHANGELOG.md`.

### v3 CLI surface consolidation

The CLI has grown to 55 subcommands and 117 unique flags. Nine of those
subcommands carry no logic of their own: each applies a named preset to a copy
of the options and delegates straight to `runStatusMany` via
`runStatusWithPreset` (`cmd/preset_helpers.go`). The preset is the whole
command body — `cmd/readiness.go` is one line.

| Command | Preset applier (`internal/cmdoptions/presets.go`) | Flags set |
| --- | --- | --- |
| `doctor` | `applyPresetDoctor` | doctor feature bundle + events/KEDA/VPA |
| `readiness` | `applyPresetReadiness` | readiness feature bundle |
| `metrics-probe` | `applyPresetMetricsProbe` | metrics-probe feature bundle |
| `preflight` | `applyPresetPreflight` | 4 |
| `container-advisor` | `applyPresetContainerAdvisor` | 4 |
| `rollout-context` | `applyPresetRolloutContext` | 6 |
| `node-context` | `applyPresetNodeContext` | 9 |
| `trace` | `applyPresetTrace` | 1 (`--decision-trace`) |
| `path` | `applyPresetPath` | 1 (`--scale-path`) |

`trace` and `path` are exactly `status --decision-trace` and
`status --scale-path`; the rest set enough flags that the long form is not a
realistic thing to type, which is precisely why they became commands.

**Decision:** keep every one of these as a discoverable entry point — the
multi-flag equivalents are not obvious and users should not have to assemble
them — but stop treating "add a command" as the way to expose a preset.
Concretely, for v3:

1. Group them under their workflow parent (`doctor readiness|rollout|capacity`,
   `advisor container|behavior|recommend`), matching the `alpha` precedent.
2. Keep the current top-level names as hidden deprecated aliases for the whole
   v3 line, printing a one-line migration hint on stderr.
3. Require new presets to ship as a `status` flag or `--analysis-profile`
   value first; a dedicated command needs a reason beyond discoverability.

This trims the top-level `--help` without removing any capability. It is
deliberately **not** a v2 change: the aliases alone are a behavior change to
every documented invocation.

**Status (v3.0.0): executed.** `doctor` is the workflow parent for
`readiness`, `rollout` (ex-`rollout-context`), `capacity` (ex-`node-context`),
`trace`, `path`, and `preflight`; `metrics probe` was already grouped;
`container-advisor` maps to the existing `advisor container`. The historical
top-level names remain as hidden deprecated aliases for the v3 line
(`cmd/deprecated_aliases.go`), printing a one-line migration hint on stderr.
New presets must ship as a `status` flag or `--analysis-profile` value first
(see the policy note in `internal/cmdoptions/presets.go`).

### v3 deprecated-facade removal

`pkg/hpa` carried 57 deprecated compatibility symbols (~900 lines) that
forwarded to their canonical domain sub-packages. All of them were removed in
v3.0.0; the migration table maps each removed symbol to its replacement:

| File | Symbols | Canonical package |
| --- | --- | --- |
| `pkg/hpa/simulate_facade.go` | 12 | `pkg/hpa/simulate` |
| `pkg/hpa/vpa.go` | 11 | `pkg/hpa/vpa` |
| `pkg/hpa/health_trend.go` | 10 | `pkg/hpa/healthtrend` |
| `pkg/hpa/readiness.go` | 9 | `pkg/hpa/readiness` |
| `pkg/hpa/churn.go` | 6 | `pkg/hpa/churn` |
| `pkg/hpa/keda.go` | 4 | `pkg/hpa/keda` |
| `pkg/hpa/workload_types.go` | 2 | `pkg/hpa/healthtrend` |
| `pkg/hpa/report_list.go` | 2 | `pkg/hpa/render` |
| `pkg/hpa/healthtrend/healthtrend.go` | 1 | in-package rename |

Removal criteria (unchanged from ARCHITECTURE.md): the canonical package is
covered at or above the facade's coverage, no in-tree caller remains, and the
release carries a migration table mapping each removed symbol to its
replacement. **Status (v3.0.0): executed.** The `facade-check` script, its
Makefile target, and its CI step were removed together with the facades.

### v3 `Analysis` storage flip

Tracked under "Slim the `Analysis` god-struct" above: make the 13 grouped
views the primary in-memory storage, flip the default wire schema from v1 to
v2, and retire the flat v1 fields. This is the third v3 item and shares the
same migration note.

**Status (v3.0.0): the default wire schema flip is executed.** Structured
status/watch output (JSON, YAML, JSONL, JSONPath, Go template) now defaults to
the grouped v2 projection; `--output-schema=v1` keeps the flat legacy shape,
and the text path is schema-independent. Still open on the v3.x line: making
the grouped views the primary in-memory storage (a wide internal refactor with
no additional user-visible change beyond what the wire flip already shipped).
The flat-field retirement itself moved to the v4 plan below.

## v4 Breaking Changes (Planned)

v3.0.0 executed everything it decided except the in-memory `Analysis` storage
flip (still open on the v3.x line, see above). v4 is planned as a
**contract-slimming release**: one CLI surface with no deprecated aliases, one
wire schema (v2), and one public Go model (the grouped views). Every item
follows the same deprecation discipline as v3 — announced on the v3 line,
executed only in the major release, and documented with a migration table in
`CHANGELOG.md`.

### v4 deprecated-alias removal

The seven hidden top-level aliases that kept working through the v3 line
(`cmd/deprecated_aliases.go`) are removed together with the file. The 3.0.0
changelog already promised this ("will be removed in the next major
release"). The grouped commands themselves are unchanged.

| Removed top-level alias | Use instead |
| --- | --- |
| `readiness` | `doctor readiness` |
| `rollout-context` | `doctor rollout` |
| `node-context` | `doctor capacity` |
| `trace` | `doctor trace` (equivalent to `status --decision-trace`) |
| `path` | `doctor path` (equivalent to `status --scale-path`) |
| `preflight` | `doctor preflight` |
| `container-advisor` | `advisor container` |

The release PR must also grep both READMEs, `docs/reference.md`, and the
asciinema demo sources for the removed short names.

### v4 `ExitNotFound` application

`cmd.ExitNotFound` (3) has been exported and reserved since the v2 line while
HPA-not-found kept exiting `1` for script compatibility. v4.0.0 flips the
`classifyError` mapping in `cmd/exitcode.go` so `ErrHPANotFound` resolves to
exit code 3, letting scripts distinguish "the HPA does not exist" from generic
API failures. The constant and the flip site already exist; the release only
removes the backwards-compatibility branch and documents the code change in
the migration table.

### v4 v1 wire-schema retirement

`--output-schema=v1` and the flat v1 projection are removed; the grouped v2
schema (`apiVersion: "hpa-status/v2"`, checked in as
`docs/output-schema-v2.json`) becomes the only structured contract. v1 was the
default through the v2 line and remains an explicit option through the v3
line, so consumers get two full major lines of notice.

- **Flag surface:** `--output-schema` keeps accepting only `v2` (and empty,
  which means v2). Removing the flag outright would break scripts that already
  pin `--output-schema=v2` a second time; flag removal waits for v5.
- **Code surface:** the v1 projection path behind `cmd/root_flags.go` /
  `cmd/options_validation.go`, the v1 JSON Schema (`docs/output-schema.json`),
  and the v1 branch of the root `output_schema_test.go` contract test.
- **Consumer impact:** JSON/JSONL/YAML consumers of the flat shape must move
  to the v2 projection or stay on the v3 line. The migration note shows one
  before/after record pair, as the 3.0.0 note did for the batch envelope.

### v4 `Analysis` flat-field retirement

Completes the "Slim the `Analysis` god-struct" refactor, sequenced so the
risky part ships with no user-visible change first:

1. **v3.x (prep):** make the 13 grouped views the primary in-memory storage;
   the flat fields become derived compatibility accessors over the groups. No
   wire or text-output change, guarded by the existing fleet benchmarks and
   golden output tests.
2. **v4.0.0 (removal):** delete the 65 flat public fields on
   `pkg/hpa.Analysis` (`pkg/hpa/types.go`) and the compatibility accessors.
   Breaking for Go importers of `pkg/hpa`; the changelog carries a migration
   table mapping each removed field group to its grouped view, in the same
   table shape the 3.0.0 facade removal used.

Removal criteria (mirroring the facade-removal policy in ARCHITECTURE.md):
grouped-view test coverage at or above the flat fields', no in-tree reader of
the flat fields, benchmark parity on the list/scan path, and the migration
table in the release notes.

### v4 extension envelope (conditional; not yet decided)

The analysis-plugin-registry decision (`docs/analysis-plugin-registry.md`)
names six preconditions for a dynamic registry and keys them to "a future
major schema version". v4 takes that decision only if all six preconditions
are demonstrably met during the v4 design phase; otherwise extensibility
stays deferred and v4 remains a pure slimming release. If an extension
envelope ships, it is a versioned, schema-described extension point with the
registry still statically typed — not a backdoor plugin system.

### Sequencing

| When | Work |
| --- | --- |
| v3.x | In-memory storage flip: grouped views primary, flat fields derived (no wire change) |
| v3.x | Additive record fields: typed numeric metric values on `TimelineSnapshot` (currently the formatted `TopMetric` string), unlocking weekly seasonality cycles in `alpha analyze-record --detect seasonality` |
| v3.x | Optional additive features that harden the v4 line: opt-in informer-based watch (see Medium Term), KEDA `spec.idleReplicaCount` extraction |
| v4.0.0 | Alias removal, v1 schema retirement, and flat-field removal in one release with a single migration section in `CHANGELOG.md` |

KEP-6111 structured decision fields stay a prepared boundary (see
ARCHITECTURE.md). If upstream ships them before v4, the adapter lands as an
additive input signal and does not block any v4 item.

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
