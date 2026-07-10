# Comprehensive Reference

Detailed reference for `kubectl-hpa-status`. For flag reference, config file, TUI key bindings, and JSONPath examples, see [Usage Guide](usage.md). For troubleshooting symptoms and FAQ, see [Troubleshooting](troubleshooting.md).

## Decision Confidence Model

Every diagnostic finding is classified into one of three evidence tiers so you can immediately distinguish facts from estimates:

| Tier | Label | Meaning | Example |
| --- | --- | --- | --- |
| Observed | `[observed]` | Directly read from HPA status fields (conditions, replicas, metrics) | `ScalingLimited=True`, `desiredReplicas == maxReplicas` |
| Estimated | `[estimated]` | Inferred from visible signals but not directly confirmable via the API | Multi-metric winner estimate, tolerance-based no-scale |
| Unknown | `[unknown]` | The Kubernetes HPA controller does not expose this information | Missing-metric dampening, not-ready pod internal adjustments |

**Why this matters:** The Kubernetes HPA controller applies conservative corrections for missing metrics and not-yet-ready pods that are not reflected in `status.currentMetrics`. The utilization values you see may differ from what the controller actually uses internally. This plugin annotates each finding so you can distinguish directly observable facts from estimates and known unknowns.

In text output, `[estimated]` and `[unknown]` lines are dimmed to draw your eye to the high-confidence `[observed]` findings first. In JSON/YAML output, each `structuredInterpretation` entry includes a `classification` field alongside the existing `confidence` field.

## Why use `kubectl-hpa-status`?

| Feature | `kubectl describe hpa` | `kubectl hpa status` (this plugin) |
| --- | --- | --- |
| **Focus** | Raw status and spec dump | Multi-perspective diagnostics with recommended actions |
| **Scaling summary** | Standard K8s condition text | Clear operational direction summary |
| **Limit detection** | Raw min/max replicas displayed | Auto-explains caps when `maxReplicas` is reached |
| **Multi-metric diagnostics** | Lists each target independently | Estimates and highlights the highest-impact metric |
| **Stabilization window alert** | Not explicitly tracked | Detects active scale-down stabilization and shows remaining wait time |
| **Watch mode** | Requires external `watch` command (no diff) | Built-in watch with highlighted deltas from previous state |
| **Recommendation guide** | None | Explains *why* and suggests configuration fixes |

### Operator workflow comparison

| Task | With `kubectl describe hpa` | With `kubectl hpa status` | Time saved |
| --- | --- | --- | --- |
| Find why an HPA won't scale | Read Conditions, Events, metrics, and replicas columns manually | `status <name> --explain` summarizes candidate causes, evidence, and next steps | Minutes during incident response |
| Find cluster-wide limit hits | `describe`/`list` per namespace and compare desired/current/max manually | `list -A --problem --sort-by problem` or `scan` prioritizes problematic HPAs | Eliminates per-namespace manual work |
| Diagnose Metrics unavailable | Guess resource/custom/external from Events | `--diagnose-metrics` shows per-metric health checks and verification hints | Shortens initial investigation |
| Explain scale-down delay | Manually correlate condition reason, behavior, and timestamps | Text/TUI shows stabilization state and estimated remaining wait | Avoids unnecessary config changes |
| Create handoff report | Paste `describe` output and annotate manually | `--report markdown` / `--report html` generates a structured report | Reduces effort for standups, audits, and incident reports |
| Safely validate a fix | Assemble `patch` command and dry-run yourself | `--suggest` / `--fix --apply` provides dry-run-first commands and warnings | Reduces patch mistakes |

## Doctor Command

Start with `doctor` when an HPA is not scaling and you need the surrounding failure context, not just the HPA object:

```sh
kubectl hpa status doctor <hpa-name> -n <namespace>
```

`doctor` bundles `--explain`, `--diagnose-metrics`, `--metrics-freshness`, `--check-resources`, `--explain-pods`, `--capacity-context`, recent Events, and KEDA enrichment.

Metrics freshness output highlights missing or stale adapter data:

```text
Metrics Freshness:
  ! keda-http-requests/external:
    status: Stale
    source: external.metrics.k8s.io
    apiservice: available (external.metrics.k8s.io/v1beta1)
    last HPA event: FailedGetExternalMetric 3m58s ago
    likely cause: KEDA trigger is inactive or authentication is failing
    evidence:
      - KEDA ScaledObject "web" is linked to this HPA
      - KEDA trigger "http" (http-requests) status=Inactive: authentication failed
    next checks:
      kubectl get apiservice | grep external.metrics
      kubectl describe hpa <name>
      kubectl get scaledobject web -n production
```

| Viewpoint | What it checks | Example output |
| --- | --- | --- |
| Metrics | metrics-server, custom metrics, external metrics | `External metric http_requests is unavailable` |
| Target workload | Deployment, StatefulSet, ReplicaSet | `Pods are Pending; HPA wants 8 replicas but only 3 Ready` |
| Pod state | Pending, CrashLoopBackOff, NotReady | `Scale-out blocked by image pull error` |
| Resource requests | Missing CPU/memory requests | `CPU utilization target cannot work because container has no cpu request` |
| Events | HPA, Pod, Deployment events | `FailedGetResourceMetric seen 5 times in 10m` |
| KEDA | ScaledObject and trigger health | `KEDA trigger inactive or auth error` |

How to read the output:

- `Summary` is the visual state derived from HPA status.
- `Recommended actions` are operational hints based on Conditions and Behavior settings.
- `Interpretation` is a diagnostic inference, not the controller's internal decision history.
- `confidence: high` means the inference is based on explicit status fields; `confidence: medium` means the status and explanation are consistent, but the API itself does not expose internal details.
- The "winner" in multi-metric scenarios is shown as an estimate. Current HPA status does not expose per-metric replica recommendations or the final selection, so the metric with the largest distance from target is highlighted.

Key signals to watch for:

- `ScalingActive=False`: Check metrics-server, custom metrics adapter, or external metrics adapter.
- `ScalingLimited=True`: Check `minReplicas`, `maxReplicas`, and target utilization.
- `ScaleDownStabilized`: Check `spec.behavior.scaleDown.stabilizationWindowSeconds` and the stabilization window.
- Output appears stale: Compare `status.observedGeneration` with `metadata.generation`.

Run `kubectl hpa status --help` after installation for the authoritative command
list. The daily command surface includes `status`, `list`, `scan`, `doctor`,
`watch`, `explain`, and `tui`; operational and experimental commands live under
`alpha`. Common flags include `-n/--namespace`, `-A/--all-namespaces`,
`-o/--output`, `--explain`, `--watch`, `--interval`, `--timeout`, `--since`, and
`--until-condition`.

## Safe Fix Flow

`--suggest` / `--fix --apply` defaults to the safe side.

```text
Observe
  kubectl hpa status <name> --explain --events=5
      |
Review suggestions only
  kubectl hpa status <name> --suggest
      |
Validate with server-side dry-run
  kubectl hpa status <name> --fix --apply
      |
Review diff, desiredReplicas, and warnings
      |
Persist changes
  kubectl hpa status <name> --fix --apply --dry-run=false
```

1. `--suggest` prints `kubectl patch` commands with `--dry-run=server`.
2. `--fix --apply` also defaults to server-side dry-run and shows `status.desiredReplicas` and the target field diff before applying.
3. Persisting changes requires explicit `--dry-run=false`.
4. maxReplicas, behavior, and tolerance suggestions include warnings about capacity, quotas, cost, feature gates, and downstream dependencies.
5. External/Object metrics prioritize adapter and target Object state verification; the plugin does not generate dangerous auto-delete patches based on status alone.

Dry-run mode differences:

- `--dry-run=server`: Sends the patch to the Kubernetes API server, validating with admission and defaulting, but does not persist.
- `--dry-run=client`: Validates locally on the kubectl side only; may miss server-side admission behavior.
- `kubectl-hpa-status --apply` defaults to server-side dry-run. Persisting changes requires `--dry-run=false`.

## Multi-Metric Decision Deep Trace

When an HPA has multiple metrics (for example CPU + memory + custom), it can be difficult to tell which metric drove the final scaling decision. The **Metric Decision Trace** provides a per-metric breakdown showing:

- Each metric's current ratio relative to target
- Whether the metric is within the tolerance band (default 10%)
- Estimated replica impact for each metric
- Which metric is the likely "winner" and at what confidence level
- The effect of stabilization window and tolerance on the scaling decision

```sh
kubectl hpa status <hpa-name> --explain --debug
```

The trace output includes:

- **Per-metric entries** with ratio, distance from target, replica impact estimate, and desired direction (up/down/none)
- **Winner detection** with confidence level (medium when not at maxReplicas, low when at maxReplicas since the winner cannot be reliably determined)
- **Stabilization effect** showing whether scale-down is suppressed and estimated remaining wait time
- **Tolerance effect** listing which metrics are suppressed by the tolerance band
- **Select policy** showing whether `Max`, `Min`, or `Least` is configured in the behavior spec

This is a best-effort estimate based on visible `currentMetrics` and `spec.metrics`. The Kubernetes HPA API does not expose per-metric replica recommendations or the controller's final metric selection.

## Structured Decision Export

Use structured export when you need machine-readable explainability or want to experiment with future KEP-6111-style HPA decision fields.

```sh
kubectl hpa status explain web -n production
kubectl hpa status web -n production --explain --format structured
```

The JSON includes schema version, per-metric target/current values, estimated desired replicas, tolerance effect, stabilization effect, limit clamp, winner metric, and an ordered decision path. It is still derived from visible Kubernetes API fields, so unavailable controller internals remain marked as estimates or unknowns.

## History and Trend

`history` combines current HPA analysis, Events-derived churn detection, health trend storage, and optional Prometheus query links:

```sh
kubectl hpa status history web -n production --since=6h --prometheus http://prometheus:9090
```

The command does not require Prometheus to be reachable. When `--prometheus` is set, it emits query_range URLs that can be opened or reused by incident tooling.

## HPA Tuning Advisor

`tune` recommends behavior, stabilization window, and tolerance settings for common goals:

```sh
kubectl hpa status tune web -n production --goal stable --suggest
kubectl hpa status tune web -n production --goal fast-scale-up --suggest
kubectl hpa status tune web -n production --goal cost-saving --suggest
```

The advisor does not mutate the cluster. Validate suggested behavior with server-side dry-run before applying, especially when using configurable tolerance fields.

## CI/CD Reports

`list` and `scan` can emit CI-friendly reports:

```sh
kubectl hpa status scan -A --report junit > hpa-health.xml
kubectl hpa status list -A --problem --report sarif > hpa-health.sarif
```

JUnit marks non-OK or low-score HPAs as failures for pipeline gates. SARIF emits HPA health findings that can be consumed by GitHub Code Scanning and similar tools.

## GitOps Drift Candidates

`list --gitops-drift` detects Argo CD / Flux-managed HPAs from labels and annotations:

```sh
kubectl hpa status list -A --gitops-drift
```

This is a live-object signal, not a Git checkout diff. For field-level manifest conflict detection, combine status analysis with `--gitops-check --manifest`.

## Local AI Context

For local LLM workflows, `--context-for-ai` and `--ask` emit a compact Markdown context pack without calling any external model:

```sh
kubectl hpa status web --context-for-ai
kubectl hpa status web --ask "why did scale-up stall?"
```

The context includes replica state, health score, conditions, metrics, hidden decision factors, and safe suggestions.

## What-If Scaling Simulator

The `--simulate-metric` flag lets you preview how an HPA would behave if a metric value changed, without modifying any cluster state.

```sh
# Simulate CPU at 80% utilization
kubectl hpa status web --simulate-metric cpu=80%

# Simulate memory at 4Gi
kubectl hpa status web --simulate-metric memory=4Gi

# Simulate an increase in http_requests by 20%
kubectl hpa status web --simulate-metric http_requests=+20%

# Combine multiple metric simulations
kubectl hpa status web --simulate-metric cpu=80% --simulate-metric memory=4Gi
```

The simulator overrides the current metric values in the analysis and shows:

- How the health score would change
- The new estimated desired replicas
- Updated interpretation and recommendations based on the simulated values

All simulation is client-side only. No changes are sent to the Kubernetes API server.

## Best Practice Auditor

The `recommend` subcommand audits HPA configuration against built-in best-practice rules and produces a compliance report with a score.

```sh
kubectl hpa status recommend <hpa-name>
```

The auditor evaluates nine rules:

| Rule | Severity | What it checks |
| --- | --- | --- |
| Stabilization window | Warning | Missing or excessively long scale-down stabilization window |
| Replica range | Critical | `minReplicas` too low (including 0) or `maxReplicas` unnecessarily high |
| Behavior policy | Warning | Missing scale-up or scale-down behavior policies |
| Metric coverage | Warning | HPA has no metrics defined or uses only a single metric type |
| Tolerance | Info | Metrics within the default tolerance band (may indicate wasted metrics) |
| Scale-to-zero | Critical | `minReplicas=0` without proper cold-start considerations |
| Resource requests | Warning | Target pods missing resource requests that the HPA depends on |
| KEDA configuration | Info | KEDA-managed HPA with potential trigger or authentication issues |
| Target utilization | Warning | Target utilization set outside recommended ranges (too high or too low) |

The compliance score starts at 100 and is reduced by:

- **Critical findings**: -20 each
- **Warning findings**: -10 each
- **Info findings**: no deduction

Example output:

```text
HPA default/web
Target: Deployment/web
Compliance Score: 70/100
Summary: Found 1 critical, 2 warnings, 0 informational findings

Audit Findings:
  [CRITICAL] minReplicas is set to 0 without scale-to-zero safeguards
  [WARNING] No scale-down behavior policy configured; defaults may cause rapid scale-down
  [WARNING] Target CPU utilization (95%) is above recommended maximum (85%)
```

## Retrospective Scaling Timeline

Show estimated past scaling decisions using the HPA decision timeline:

```bash
kubectl hpa status timeline web -n production --since=30m
```

Output:

```text
HPA Scaling Timeline: web (production)  since 30m ago

21:05:00 CPU 92% > target 60%     desired 3 -> 5
21:06:00 ScalingLimited=True      capped by maxReplicas=5
21:10:00 FailedGetResourceMetric  metrics unavailable
21:15:00 ScaleDownStabilized      scale-down suppressed, ~180s remaining

Note: Best-effort reconstruction from Kubernetes events and current HPA status.
```

Limitations:
- The HPA controller's internal decision history is not fully visible through the Kubernetes API
- Multi-metric winner determination is estimated
- Exact metric values at decision time are not available
- Suppressed scaling decisions that did not produce events may be missing
- Kubernetes events typically expire after ~1 hour, so `--since` values beyond that may return empty results

Supports all output formats: `--since=30m -o json`, `--since=30m -o yaml`, `--since=30m --report markdown`, `--since=30m --report html`.

## Durable Decision Recording

`record` persists visible HPA decision snapshots to JSONL so you can analyze behavior after Kubernetes Events expire:

```sh
kubectl hpa status record -A --interval=15s --duration=1h -o hpa-history.jsonl
kubectl hpa status timeline web -n production --from-record hpa-history.jsonl
```

The record file stores one compact trace per HPA per polling cycle. At shutdown, the command prints how many snapshots were captured and highlights interesting changes such as desired replica changes, health transitions, score changes, and condition changes.

Use this when you need a durable answer to "what changed around the time this HPA did not scale?" The data is still based on visible HPA status, conditions, metrics, and events; it does not expose private controller internals.

## Capacity and Quota Preflight

Before applying a maxReplicas fix, validate whether the target workload can actually run the additional pods:

```sh
kubectl hpa status preflight web -n production --raise-max 20
```

`preflight` reuses the capacity plan engine and checks namespace ResourceQuota, LimitRange constraints, node allocatable summary, Pending pods, PDB signals, and Cluster Autoscaler detection. The older `capacity` command remains available for the same standalone capacity report.

## Metrics Adapter Probe

Use `metrics probe` when a custom, pods, object, or external metric appears missing or stale:

```sh
kubectl hpa status metrics probe web -n production
```

The probe combines current HPA metric status, metric freshness, API discovery, metric contract checks, adapter diagnostics, and metric hints. This is intended to narrow the problem to metrics-server, custom.metrics.k8s.io, external.metrics.k8s.io, KEDA metric naming, selector mismatch, or stale samples.

## Behavior Visualizer

Use `behavior` to explain how `spec.behavior.scaleUp` and `scaleDown` policies shape the path from current replicas to desired replicas:

```sh
kubectl hpa status behavior web -n production
```

Output includes stabilization windows, selectPolicy, policies such as `+100% per 15s` or `+4 pods per 15s`, and a best-effort estimated path like `t+15s: 20`.

## Cost and Availability Estimate

Use `estimate` to quantify the rough upper-bound pod and cost impact of changing maxReplicas:

```sh
kubectl hpa status estimate web -n production --max-replicas 30 --pod-cost 0.12
```

The command reports current maxReplicas, proposed maxReplicas, additional worst-case pods, and optional hourly cost. It is deliberately simple; run `preflight` before applying the change to validate quota and capacity.

## CI and Cluster Summary Reports

Offline lint supports both SARIF and GitHub Actions annotations:

```sh
kubectl hpa status lint -f k8s/ -o sarif
kubectl hpa status lint -f k8s/ -o github
```

Cluster scan can generate a management-friendly report with totals, worst HPAs, and prioritized actions:

```sh
kubectl hpa status scan -A --report markdown --summary
kubectl hpa status scan -A --report html --summary
```

## GitOps Patch Export

Safe suggestions can be exported without directly patching the live cluster:

```sh
kubectl hpa status web -n prod --suggest --export yaml
kubectl hpa status web -n prod --suggest --export kustomize
kubectl hpa status scan -A --problem --export directory
```

The single-HPA formats print a minimal `autoscaling/v2` HPA patch document or Kustomize/Helm-friendly snippet. The `directory` mode writes one YAML patch per HPA under `hpa-patches/` for PR workflows.

## Hidden Decision Factors

`--hidden-factors` surfaces controller inputs that affect HPA decisions but are only partially visible in public status:

```sh
kubectl hpa status web -n prod --hidden-factors
```

Examples include missing current metrics, ratios inside the tolerance band, active stabilization, and not-yet-ready pod effects. Each factor includes evidence, impact, and confidence so operators can distinguish observed facts from estimates or unknowns.

The same output also includes `Score Breakdown`, which lists the health score base, every penalty signal, and the final score.

## Drift Compare

Compare HPA configuration between contexts or environments:

```sh
kubectl hpa status compare stg/web prod/web -n app
kubectl hpa status compare -A --from-context stg --to-context prod --only-drift
```

The comparison includes min/max replicas, metric targets, scale-down stabilization, and health score. The all-namespace mode matches HPAs by `namespace/name` and reports only drift when `--only-drift` is used.

## Alerts and Record Analytics

Generate starter alert rules from `kubectl-hpa-status` health semantics:

```sh
kubectl hpa status alerts generate --format prometheus
kubectl hpa status alerts generate --format datadog
```

Analyze durable record files for flapping:

```sh
kubectl hpa status alpha analyze-record hpa-history.jsonl --detect flapping
```

The record analyzer counts desired replica changes and direction flips, then suggests stabilization/tolerance review when oscillation is detected.

## Interactive TUI

For the full TUI workflow, key bindings, export guidance, and troubleshooting notes, see [TUI Manual](tui.md). For the shorter flag and key reference, see [Usage Guide - Interactive TUI](usage.md#interactive-tui).

Quick reference:

```sh
kubectl hpa status tui          # Current namespace
kubectl hpa status tui -A       # All namespaces
kubectl hpa status web --watch --dashboard
```

The dashboard auto-refreshes every 5 seconds by default; change the interval with `--interval`. Press `g` to jump to the first HPA with a problem. Press `m` for per-metric diagnostics. Press `space` to select multiple HPAs before entering the batch audit or CLI export workflow.

## Troubleshooting Patterns

For the complete symptom/command table and FAQ, see [Troubleshooting](troubleshooting.md).

## Health Score Details

For the full health score deduction table and health state definitions, see [Usage Guide - Health Score](usage.md#health-score).

## Compatibility Matrix

This plugin uses `autoscaling/v2`. Support is split into four layers so the requirement, the tested range, the CI matrix, and the client-go dependency are each unambiguous:

| Layer | Version | Meaning |
| --- | --- | --- |
| API availability | Kubernetes 1.23+ | `autoscaling/v2` exists (GA in 1.23). The plugin *may* load an HPA here, but these versions are below the officially supported range. |
| Official support | Kubernetes 1.26+ | The stable API line this project documents as the requirement and exercises in CI. |
| CI matrix | 1.26, 1.28, 1.30, 1.35 | kind E2E matrix in `.github/workflows/ci.yml`. 1.36 is omitted only because a `kindest/node` image is not yet published. |
| client-go | see `go.mod` (`k8s.io/client-go`) | The client library version the plugin is built against; tracked separately from the server range. |

It is expected to work on future Kubernetes versions as long as `autoscaling/v2` is available.

| Environment | Status |
| --- | --- |
| HPA API `autoscaling/v2` | Required |
| metrics-server (pinned in CI) | Verified on kind |
| custom/external metrics adapters | Supported within what HPA status exposes. Ratio and selector interpretation is best-effort; adapter internal state is not directly inspected. |
| KEDA 2.0+ (`keda.sh/v1alpha1`) | Auto-detects KEDA-managed HPAs. With `--keda`, references ScaledObject showing trigger type, metric name, threshold, current value, auth ref, polling interval, cooldown, and fallback config. |
| VPA 0.9+ (`autoscaling.k8s.io/v1`) | With `--vpa`, detects CPU/Memory dual management on the same target and shows visible recommendations when VPA CRD is present. |
| Shell Completion | Supports bash, zsh, fish, and PowerShell. Includes dynamic completion for HPA names, namespaces, and contexts. |

## Verified Environments

- kind: v0.31.0
- kind node image: `kindest/node:v1.35.0`
- Kubernetes server: v1.35.0
- kubectl: v1.36.1
- metrics-server: v0.8.1
- HPA API: `autoscaling/v2`

metrics-server was verified using the upstream release manifest with the `--kubelet-insecure-tls` option added for kind.

## Validation Matrix

| Case | Explainable from existing signals? | Signals used | Remaining ambiguity |
| --- | --- | --- | --- |
| CPU above target, ScaleUp | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Low |
| CPU below target, ScaleDown | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Low |
| Limited by `maxReplicas` | Yes | `ScalingLimited`, `maxReplicas` | Low |
| Metric retrieval failure | Yes | `ScalingActive=False`, Events | Low |
| Multi-metric final winner | Partially difficult | `currentMetrics`, `spec.metrics` | Medium |
| ScaleDown stabilization | Partially possible | `AbleToScale`, condition reason, Events | Medium |
| No-scale due to tolerance | Difficult | `currentMetrics`, `desiredReplicas` | Medium to high |
| Impact of missing metrics / not-ready pods | Difficult | Insufficient in current status | High |

Events are useful as recent diagnostic context, but this POC does not treat them as a stable decision record.

## Output Examples

List view:

```text
NAMESPACE            NAME                             CURRENT  DESIRED  HEALTH              SCORE    ISSUE                            SUMMARY
default              web                              3        5        🟢 Healthy          100                                       HPA currently wants to scale up.
default              api                              2        2        🔴 ERROR            55       ERROR: FailedGetResourceMetric   HPA cannot currently compute a scaling recommendation from metrics.
```

Multi-metric HPA:

```text
HPA default/web-multi
Target: Deployment/web-multi
Replicas: current=5 desired=5 min=2 max=5
Health score: 🔴 ScalingLimited 75/100
Summary: HPA is at maxReplicas.

Metrics:
  - Resource cpu current=0% target=80% note="current value is below target"
  - Resource memory current=68% target=50% note="current value is above target"

Recommended actions:
  - HPA is capped at maxReplicas; raise maxReplicas or reduce load/target utilization if more capacity is expected.

Recommended commands:
  - Raise maxReplicas: The HPA is capped at maxReplicas=5. Raising it to 10 allows the controller to add capacity if metrics still require it. (risk: medium)
    $ kubectl patch hpa web-multi -n default --type=merge -p '{"spec":{"maxReplicas":10}}'

Interpretation:
  - [confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.
  - [confidence: medium] Among visible resource utilization metrics, memory has the largest distance from target (ratio 1.360).
  - [confidence: high] This is only an impact estimate; the API does not expose per-metric replica recommendations or the final metric winner.
```

## Findings

Even with existing HPA signals alone, the following are well explained:

- Metric retrieval failure via `ScalingActive=False`, condition reason, and recent Events
- Limit hit via `ScalingLimited=True`, condition reason, and `desiredReplicas == maxReplicas`
- Visible ScaleUp / ScaleDown direction from `currentReplicas` and `desiredReplicas`
- ScaleDown stabilization surfaced via condition reason like `ScaleDownStabilized`

However, some aspects remain difficult to determine from HPA status alone:

- Which metric determined the final recommendation in a multi-metric HPA
- Whether no-scaling is due to tolerance, rounding, or conservative metric handling
- How missing metrics or not-ready pods influenced the internal recommendation
- The internal recommendation history used for stabilization

## Known Gaps

This plugin displays what can be inferred from HPA status, metrics, conditions, and events.
It does not have access to the controller's intermediate calculations or private decision history.
Interpretation lines include confidence levels to distinguish directly observable facts from weaker inferences.

## Limitations

- The Kubernetes HPA API does **not** expose the controller's internal scaling decision trace.
- Multi-metric "winner" detection is a best-effort estimate from visible `currentMetrics`.
- Tolerance, missing-metric dampening, not-ready pod effects, and stabilization's internal recommendation history are not fully exposed in HPA status.
- Events are diagnostic hints, not a durable structured decision log.

## CI/CD

| Workflow | Purpose |
| --- | --- |
| [ci.yml](../.github/workflows/ci.yml) | `go test`, coverage, govulncheck, gosec, golangci-lint, kind E2E |
| [codeql.yml](../.github/workflows/codeql.yml) | CodeQL static analysis |
| [release.yml](../.github/workflows/release.yml) | GoReleaser for binaries, SBOM, Homebrew Cask Tap updates, and Krew release bot |

CI uploads coverage to Codecov. Release Homebrew updates use the dedicated Tap [mattsu2020/homebrew-kubectl-hpa-status](https://github.com/mattsu2020/homebrew-kubectl-hpa-status).
E2E runs on a matrix of Kubernetes 1.26 / 1.28 / 1.30 / 1.35 kind images to continuously verify `autoscaling/v2` compatibility across the supported range. (1.36 is omitted only because a `kindest/node` image is not yet published; see the workflow file.) The metrics-server, KEDA CRD, and VPA CRD installed during E2E are pinned to explicit release tags for reproducibility.

## Deprecated and Legacy Features

This project follows a gradual deprecation policy: features are first marked `Deprecated` in their CLI help text, remain functional with a deprecation notice, and are removed in the next major version.

| Feature | Status | Replacement | Removal Target |
| --- | --- | --- | --- |
| `analyze` subcommand | Removed in v2.0 | `status NAME --explain` | Complete |
| `--recommend`, `--export-patch`, `--max-score` | Removed in v2.0 | `--suggest`, `--export`, `--health-score` | Complete |
| Top-level operational aliases | Removed in v2.0 | `alpha <command>` | Complete |

### Migration

Replace any `kubectl hpa analyze NAME` invocation with the equivalent status command:

```bash
# Before
kubectl hpa analyze my-hpa

# After
kubectl hpa status my-hpa --explain
```

## Feature Status

### Available Now

**Status & Diagnostics:**
- `status --explain` — HPA status with interpretation and recommended actions
- `doctor` — Full diagnostics bundling metrics, workload, pod, resource, event, capacity, and KEDA analysis
- `list -A --problem --sort-by problem` — Cluster-wide HPA inventory with health scores
- `scan` — Prioritized cluster triage for problematic HPAs
- `timeline --since=30m` — Retrospective scaling timeline reconstructed from events
- `--diagnose-metrics` — Per-metric health checks and adapter verification hints
- `--metrics-freshness` — Staleness detection for HPA currentMetrics
- `--check-resources` — Consistency check between HPA targets and pod resource requests
- `--debug` / `-v` — Internal calculation details including metric ratio and condition evidence

**Safe Fix & Automation:**
- `--suggest` — Concrete `kubectl patch` commands with server-side dry-run
- `--fix --apply` — Patch preview with diff, warnings, and confirmation prompt
- `--dry-run=false` — Explicit opt-in to persist changes
- `list -A --problem --fix --apply` — Batch fix for multiple HPAs

**Analysis & Simulation:**
- Multi-Metric Decision Deep Trace — Per-metric ratio, tolerance/stabilization impact, winner estimate
- `--simulate-metric` — Client-side what-if scaling preview without cluster changes
- `recommend` — Best Practice Auditor with 9-rule compliance scoring

**Integrations:**
- KEDA — Auto-detects ScaledObjects; shows trigger type, metric name, threshold, current value, auth ref, polling interval
- VPA — Detects CPU/Memory dual management conflicts
- `--report markdown` / `--report html` — Standalone diagnostic reports for single or cluster-wide analysis
- JSONPath / Go template / JSON / YAML output for automation

**Interactive:**
- TUI dashboard — Real-time monitoring with cursor navigation, filtering, sorting, metric detail view, and multi-select
- `watch --interval 5s --until-condition` — Watch with highlighted deltas from previous state
- Shell completion for bash, zsh, fish, PowerShell (HPA names, namespaces, contexts)
- Japanese labels (`--lang=ja`)

**Distribution:**
- Krew plugin (`kubectl krew install hpa-status`)
- Homebrew Cask via dedicated Tap
- GoReleaser binaries with SBOM

### Experimental

These features are available but have known limitations — see [Limitations](#limitations) for details.

- **Multi-metric winner detection:** Best-effort estimate from visible `currentMetrics`. The HPA API does not expose per-metric replica recommendations or the controller's final metric selection. Confidence levels are attached to each inference.
- **TUI multi-select batch apply:** `space` / `a` / `A` for selection and CLI batch apply guidance. In-TUI direct apply is not yet available.
- **Retrospective timeline:** Reconstructed from Kubernetes events which typically expire after ~1 hour. Suppressed scaling decisions that did not produce events may be missing.
- **Durable record timeline:** `record` persists visible snapshots for later replay, but it still cannot expose controller-internal recommendations that are not published through the Kubernetes API.

### Planned

Planned work is tracked in [ROADMAP.md](../ROADMAP.md) to keep long-term plans in one place.

## Demo Recordings

Asciinema recordings (`.cast`) can be viewed on [asciinema.org](https://asciinema.org) or converted to animated SVGs.

| Command | Recording | SVG |
| --- | --- | --- |
| `status --explain` | [status-explain.cast](status-explain.cast) | [status-explain.svg](../images/status-explain.svg) |
| `doctor` full diagnostics | [doctor.cast](doctor.cast) | [doctor.svg](../images/doctor.svg) |
| `list -A --wide` | [list-wide.cast](list-wide.cast) | [list-wide.svg](../images/list-wide.svg) |
| `scan` cluster triage | [scan.cast](scan.cast) | [scan-demo.svg](../images/scan-demo.svg) |
| `timeline --since=30m` | [timeline.cast](timeline.cast) | [timeline.svg](../images/timeline.svg) |
| `recommend` audit | [recommend.cast](recommend.cast) | [recommend.svg](../images/recommend.svg) |
| `--simulate-metric` what-if | [simulate.cast](simulate.cast) | [simulate.svg](../images/simulate.svg) |
| TUI interactive dashboard | [tui.cast](tui.cast) | [tui.svg](../images/tui.svg) |
| `watch --interval 5s` | [watch.cast](watch.cast) | [watch-mode.svg](../images/watch-mode.svg) |
| `--suggest` → `--fix --apply` | [fix-flow.cast](fix-flow.cast) | [apply-diff.svg](../images/apply-diff.svg) |

- Screenshot: [images/demo.png](../images/demo.png)
- Comparison image: [images/describe-vs-hpa-status.svg](../images/describe-vs-hpa-status.svg)
- Zenn article draft: [docs/zenn-hpa-status-ja.md](zenn-hpa-status-ja.md)

Social preview source file: [images/social-preview.svg](../images/social-preview.svg)
