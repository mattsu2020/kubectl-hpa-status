# Analysis Storage Flip — Execution Design

Status: **executed** (Stages A–D landed on the v3.x line). This document
records the design and the as-executed outcome of the "v3 `Analysis` storage
flip" tracked in `ROADMAP.md` ("Slim the `Analysis` god-struct" / "v4
Breaking Changes (Planned)"): the 13 grouped views are the primary in-memory
storage, the flat v1 fields became accessor methods, and v4.0.0 only has to
delete. No wire bytes, text output, or exit codes changed.

## Why a written design

The flip is a wide internal refactor (~1,300 in-tree edit sites, measured
below) with zero user-visible payoff on the v3 line. Its risk is drift: a
field copied to the wrong group, a read that silently changes semantics, a
JSON key that moves. The mitigation was to (1) decouple the wire from the
storage first (Stage A), (2) execute the sweep as one types-aware mechanical
rewrite, and (3) gate every step on the existing golden/contract/benchmark
suite. The two hazards the design anticipated and the sweep handled:

- **Method-value hazard**: after fields become methods, a missed read site
  (`a.Current` in a `%v`/`any` context) compiles as a method value and breaks
  silently. The sweep therefore rewrote every Analysis-typed selector via
  `go/types` receiver resolution rather than waiting for compile errors.
- **Reflection renderers**: jsonpath/Go templates walk struct fields, not
  methods; `internal/render` now projects Analysis-bearing report values to
  the flat v1 shape (`projectForReflection`) before executing expressions.

## Measured migration surface (2026-08, main @ e0bf05b)

Measured by renaming all exported flat fields of `Analysis` and enumerating
compiler errors (`go build -gcflags=-e ./...`), plus grep for chained access
in `cmd/` and `internal/`:

| Surface | Size | Notes |
| --- | --- | --- |
| `pkg/hpa` non-test | 479 errors / 22 files | All flat-field traffic in the analysis core. Largest: `analysis_groups.go` (66), `text_sections.go` (61), `analysis_phases.go` (46), `text_extras.go` (41), `text.go` (35) |
| `cmd/` + `internal/` non-test | ~250 sites | Mostly chained reads (`report.Analysis.Health`) |
| Test code | ~600 sites | 165 `Analysis{` literals, 95 `StatusReport{` literals, ~350 receiver-pattern accesses |
| Renderers | 4 type switches | `internal/render`: Prometheus/Markdown/HTML/Incident assert `hpaanalysis.StatusReport` / `[]StatusReport`; jsonpath/go-template reflect over field names |

As executed: the mechanical pass applied **1,525 edits across 109 files**
(literal wraps into `NewAnalysis(FlatAnalysis{...})`, reads into `X.Field()`,
writes into `X.SetField(v)`) plus a handful of manual fixes (multi-assign
sites, one non-addressable receiver, package qualification of generated
constructor calls outside `pkg/hpa`).

## As-executed design

- **Storage**: `Analysis` holds the 13 grouped views as unexported fields
  plus a private `creationTimestamp metav1.Time` (full fidelity; `MetaView`
  serializes it as second-precision RFC3339 for v2). The view reader methods
  (`Meta`, `Replicas`, `MetricsGroup`, ...) return the stored groups;
  `Grouped()` composes a copy.
- **Compatibility accessors** (`analysis_accessors.go`): one getter and
  `Set*` setter per retired field, same names (`Current()`/`SetCurrent`),
  pointer receivers throughout. `TestFlatAccessorSurface` enforces the
  accessor surface matches `FlatAnalysis` field-for-field with types.
- **Construction**: `NewAnalysis(FlatAnalysis{...}) *Analysis` replaces flat
  composite literals; `TestNewAnalysisFlatRoundTrip` and
  `TestAccessorsReturnFixtureValues` pin the storage↔projection fidelity on
  a fully populated fixture.
- **Wire**: unchanged — `MarshalJSON` routes through `Flat()` (Stage A), and
  the flip added `UnmarshalJSON` so decoding v1 JSON into `Analysis` still
  reproduces the flat values (in-tree integration tests and external
  consumers rely on this).
- **Renderers**: `internal/render` projects `StatusReport`/`[]StatusReport`/
  `StatusBatch` to their V1 envelopes for jsonpath and Go templates
  (`projectForReflection`); the type-asserting renderers keep receiving the
  live values.

## Verification outcome

- `go test ./...` fully green, including the golden/contract suites and the
  root `output_schema_test.go` end-to-end comparisons.
- Benchmarks vs main: `BenchmarkAnalyze` ~7.2µs (main ~7.0µs),
  `BenchmarkAnalyzeNoInterpret` ~2.8µs (main ~2.55µs, ≈+9% ≈ +250ns per
  analysis from group writes and accessor indirection), suggestions/lint
  benchmarks within noise. Against the network cost of every real CLI
  invocation this is negligible; recorded here for the parity gate.
- `golangci-lint` clean on all changed packages (the pre-existing
  `cmd/apply.go` gocyclo remains, see below).

## Known local-gate mismatches (pre-existing, not flip-related)

`make lint` flags `cmd/apply.go` gocyclo 16 (>15) and `make coverage-check`
reports mutating command paths 65.1% (<67%) **on clean main** (verified via
stash on 2026-08-20). CI presumably runs a pinned golangci-lint/coverage
setup that agrees with main. Do not treat these as flip regressions; do not
"fix" them inside flip PRs.

## v4.0.0 removal (ready)

With no in-tree flat readers left, v4.0.0 deletes the accessor methods (or
the whole flat surface) and ships the migration table mapping each retired
field to its grouped view (same table shape the 3.0.0 facade removal used):

| Retired Go API | v4 replacement |
| --- | --- |
| `a.Current`, `a.Desired`, `a.Min`, `a.Max`, `a.TargetReplicas` | `a.Replicas().Current`, ... (or the `ReplicasView` group) |
| `a.Health`, `a.HealthScore`, `a.Summary`, `a.SummaryKey`, `a.ImpactMetric`, traces, signals | `a.Decision()` group |
| `a.Namespace`, `a.Name`, `a.Target`, `a.CreationTimestamp` | `a.Meta()` group |
| `a.Metrics`, diagnostics/freshness/contract/hints/adapter | `a.MetricsGroup()` group |
| `a.Conditions`, `a.Behavior`, stabilization fields | `a.ConditionsGroup()` group |
| `a.Actions`, `a.Suggestions`, structured/interpretation/assumptions/warnings | `a.ActionsGroup()` group |
| `a.StaleStatus`, `a.HealthTrend`, `a.Debug`, `a.HiddenFactors`, `a.EnrichmentStatus` | `a.Lifecycle()` group |
| capacity/pod/scale-path/readiness fields | `a.Capacity()` group |
| `a.ScaleToZero`, `a.WarmupAnalysis` | `a.ScaleToZeroGroup()` group |
| simulation/prevention/diagnosis/churn | `a.Stability()` group |
| VPA/container/behavior advisors | `a.Advisory()` group |
| `a.KEDAInfo`, `a.RolloutDiagnosis`, `a.ControllerProfile` | `a.Controllers()` group |
| `a.BlockerReport`, `a.GitOpsConflict` | `a.Blockers()` group |
| setters (`a.SetX(...)`) | write the group: `a.Replicas().Current` is a copy — use `NewAnalysis(FlatAnalysis{...})` or targeted setters retained at v4's discretion |
| `&Analysis{field: v}` literals | `NewAnalysis(FlatAnalysis{field: v})` (available now) |

The v1 wire retirement (also v4) then removes `--output-schema=v1`,
`FlatAnalysis`, the V1 projections, and `MarshalJSON`/`UnmarshalJSON` in one
commit — the schema contract test pins move to the v2 schema only.
