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
| `cmd/` | Cobra commands, flags, Kubernetes client orchestration, output format routing |
| `internal/kube/` | kubeconfig resolution, client construction, test helpers |
| `internal/style/` | terminal color and semantic styling |
| `pkg/hpa/analysis.go` | HPA signal extraction, summaries, interpretation, health scoring |
| `pkg/hpa/suggestions.go` | safe patch suggestion generation |
| `pkg/hpa/text.go` | human-readable status, list, and watch output |
| `pkg/hpa/events.go` | recent Event lookup and formatting |
| `test/e2e/` | kind-backed command path tests |

### cmd/ file responsibilities

| File | Purpose |
| --- | --- |
| `root.go` | Root command, flag definitions, config file parsing, shared `options` struct |
| `status.go` | `status` and `analyze` (deprecated) subcommands, HPA fetch, KEDA/VPA enrichment |
| `list.go` | `list`/`scan` subcommands, sort/filter logic |
| `apply.go` | `--apply` suggestion workflow with confirmation and patch diff |
| `watch.go` | Polling watch loop for status and list |
| `tui.go` | Bubble Tea interactive TUI dashboard |
| `output.go` | Format routing (JSON, YAML, jsonpath, template, prometheus), config loading |
| `helpers.go` | `eventOption` type for `--events` flag |
| `exitcode.go` | Exit code constants for script integration |
| `completion.go` | Shell completion generation and HPA name completion |

Potential refactoring notes:
- `output.go` handles both format routing and config loading. Config loading could move to a dedicated `config.go`.
- `status.go` is the largest file and handles KEDA/VPA enrichment. Enrichment helpers could move to `internal/kube/`.
- The `options` struct in `root.go` is shared across all commands. As flags grow, consider splitting into per-command option structs.

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

Health states (in priority order): `ERROR` > `LIMITED` > `STABILIZED` > `OK`.

Score is clamped to [0, 100]. All penalties are configurable via `--health-weights` flag or config file.

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

`--watch` remains a simple polling loop over Kubernetes API reads. The
`--dashboard` renderer is intentionally output-only: it does not introduce an
event loop framework or terminal input state. If a future Bubble Tea-style TUI
is added, it should reuse the same `Analysis` model and keep JSON/YAML output
unchanged.

## KEDA And Adapter Context

KEDA and custom/external metrics adapter support is currently limited to
signals visible on the HPA itself. The analyzer detects KEDA-style labels,
annotations, and `keda-hpa-*` names, then points operators to ScaledObject and
adapter diagnostics. Direct reads of KEDA CRDs should be added through a
separate optional client path so clusters without KEDA do not pay that cost.

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

Concrete integration plan:

- Add a small adapter that converts the new structured decision fields into the
  existing `Analysis` model instead of leaking raw API shape into renderers.
- Prefer structured controller reasons over current best-effort inference for
  metric winner, tolerance, missing-metric handling, and stabilization history.
- Keep the old inference path as a fallback for older clusters and mark it with
  lower confidence when structured decisions are absent.
- Extend JSON/YAML output with additive fields only; do not rename existing
  fields such as `summary`, `conditions`, `metrics`, or `suggestions`.
- Add fixture tests that compare the same HPA with and without structured
  decision data so behavior remains compatible across Kubernetes versions.
