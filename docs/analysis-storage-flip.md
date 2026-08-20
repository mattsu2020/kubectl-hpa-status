# Analysis Storage Flip — Execution Design

Status: Stage A executed; Stages B–D pending. This document is the execution
plan for the "v3 `Analysis` storage flip" tracked in `ROADMAP.md` ("Slim the
`Analysis` god-struct" / "v4 Breaking Changes (Planned)"): make the 13 grouped
views the primary in-memory storage, turn the flat v1 fields into derived
compatibility accessors, and retire the flat fields in v4.0.0. Nothing in
this plan changes any wire bytes, text output, or exit codes.

## Why a written design

The flip is a wide internal refactor (~1,300 in-tree edit sites, measured
below) with zero user-visible payoff on the v3 line. Its risk is drift: a
field copied to the wrong group, a read that silently changes semantics, a
JSON key that moves. The mitigation is to (1) decouple the wire from the
storage first, (2) execute in compile-green stages, and (3) gate every stage
on the existing golden/contract/benchmark suite. Stage A (the decoupling)
has landed; this file records what remains so the follow-up sessions are
mechanical.

## Measured migration surface (2026-08, main @ e0bf05b)

Measured by renaming all exported flat fields of `Analysis` and enumerating
compiler errors (`go build -gcflags=-e ./...`), plus grep for chained access
in `cmd/` and `internal/`:

| Surface | Size | Notes |
| --- | --- | --- |
| `pkg/hpa` non-test | 479 errors / 22 files | All flat-field traffic in the analysis core. Largest: `analysis_groups.go` (66), `text_sections.go` (61), `analysis_phases.go` (46), `text_extras.go` (41), `text.go` (35) |
| `cmd/` + `internal/` non-test | ~250 sites | Mostly chained reads (`report.Analysis.Health`); no compile-error number because dependent packages were skipped once `pkg/hpa` broke |
| Test code | ~600 sites | 165 `Analysis{` literals, 95 `StatusReport{` literals, ~350 receiver-pattern accesses |
| Renderers | 4 type switches | `internal/render`: Prometheus/Markdown/HTML/Incident assert `hpaanalysis.StatusReport` / `[]StatusReport` and read flat fields directly; jsonpath/go-template reflect over field names |

Nothing outside the module imports `pkg/hpa` (kubectl plugin; the Go API is
a convenience surface), so external breakage is limited to the v4.0.0
migration table.

## Stage A — v1 wire decoupling (done)

Landed with `pkg/hpa/schema_v1.go`:

- **`FlatAnalysis`** mirrors the 65 exported flat fields of `Analysis` in
  exact declaration order with exact tags, so `encoding/json` and
  `sigs.k8s.io/yaml` (JSON-first) emit identical keys in identical order.
  `schema_v1_test.go` enforces the mirror by reflection in both directions:
  editing one struct without the other fails the build's tests.
- **`Analysis.Flat()`** builds the flat value by *inverting* `Grouped()` —
  not by copying the flat fields. The inverse mapping is the exact code the
  flip depends on; it is written and property-tested (field-for-field
  round-trip against a fully populated `Analysis`) *before* storage changes.
  The one view divergence — `metav1.Time` vs `MetaView`'s RFC3339 string —
  is handled by `parseMetaTimestamp`; both wire formats serialize
  second-precision RFC3339, so the round trip is faithful.
- **`Analysis.MarshalJSON`** routes every JSON/YAML consumer through
  `Flat()`. This is the load-bearing piece: once serialization derives from
  the projection, the flip may rearrange `Analysis`'s in-memory layout
  arbitrarily without touching a single output byte. Decoding is unaffected
  (Unmarshal ignores MarshalJSON, and the flat fields still exist through
  v3).
- **Explicit v1 emit path**: `ProjectStatusReportV1` / `ProjectStatusBatchV1`
  / `ProjectStatusReportsV1`, wired into `statusOutputValue`/`batchValue`
  for json/yaml/jsonl. Renderer formats that type-assert `StatusReport`
  (prometheus/markdown/html/incident) keep receiving the live value.
- **Contract pin**: `output_schema_test.go` checks `docs/output-schema.json`'s
  `analysis` definition against `FlatAnalysis`, not `Analysis` — with the
  custom marshaller, `Analysis`'s own tags no longer describe the wire.

Byte-identity is gated by `TestProjectStatusReportV1IsByteIdentical` plus the
pre-existing golden/contract tests, which pass unchanged.

## Stage B — pkg/hpa core conversion

Goal: inside `pkg/hpa`, flat fields stop being read or written; the groups
are the working representation. Mechanics:

1. Add unexported group storage to `Analysis` (13 view-struct fields).
   Unexported storage avoids the promoted-field/method name collisions; the
   existing view methods (`Meta()`, `Replicas()`, `MetricsGroup()`, ...)
   become storage readers, `Grouped()` returns the storage (copy).
2. Add flat compatibility accessors — getter methods named exactly like the
   retired fields (`Current()`, `Desired()`, `Health()`, ...) plus setters
   (`SetCurrent(v)`) for writer sites. The one name that cannot stay is
   `Metrics` (collides with the view method surface); it becomes
   `MetricsList()` / `SetMetricsList()` and the v4 migration table says so.
3. Convert the 479 non-test sites compiler-driven, file by file:
   reads `a.X` → `a.X()`, writes `a.X = v` → `a.SetX(v)`,
   `append(a.X, ...)` → `a.SetX(append(a.X(), ...))`. At this stage the
   accessors still delegate to the flat fields, so behavior is trivially
   unchanged and the package compiles after every file.
4. Delete the flat fields; accessors read/write the group storage instead.
   `Flat()` and `MarshalJSON` are untouched (Stage A guarantee).

Gates: `go test ./pkg/hpa/...`, `BenchmarkAnalyze*` / list-path benchmarks
unchanged within noise, coverage of `analysis_groups.go` not below its
current level.

## Stage C — cmd / internal conversion

Same mechanical transform for the ~250 chained sites
(`report.Analysis.Health` → `report.Analysis.Health()`) and the four
renderer type switches. Renderers may either read groups via
`CanonicalAnalysis()` (preferred — they already exist on `StatusReport`) or
accept `FlatAnalysis` values. `statusOutputValue`/`batchValue` already emit
projections for the structured formats, so no byte-level risk here either.

Gates: `go test ./...` (root contract test included), `make docs-check`.

## Stage D — test conversion and cleanup

The ~600 test sites: literals become either group-literal construction or a
small test builder next to `baseHPA()`. After conversion, delete whatever
compat shims the earlier stages temporarily introduced
(`FreezeCanonical`'s frozen snapshot collapses once groups are live storage
— keep the method as an immutability guard or remove it with its callers).

Gates: full `make ci` equivalents that pass on main (see "Known local-gate
mismatches" below), plus a coverage-parity diff on the touched packages.

## Known local-gate mismatches (pre-existing, not flip-related)

`make lint` flags `cmd/apply.go` gocyclo 16 (>15) and `make coverage-check`
reports mutating command paths 65.1% (<67%) **on clean main** (verified via
stash on 2026-08-20). CI presumably runs a pinned golangci-lint/coverage
setup that agrees with main. Do not treat these as flip regressions; do not
"fix" them inside flip PRs.

## v4.0.0 removal (after the flip lands)

With no in-tree flat readers left, v4.0.0 deletes the flat accessor methods
and ships the migration table mapping each retired field group to its
grouped view (same table shape the 3.0.0 facade removal used). The v1 wire
retirement (also v4) then removes `--output-schema=v1`, `FlatAnalysis`, the
V1 projections, and `MarshalJSON` in one commit — the schema contract test
pins move to the v2 schema only.
