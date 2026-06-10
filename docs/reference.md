# Comprehensive Reference

Detailed reference for `kubectl-hpa-status`. For flag reference, config file, TUI key bindings, and JSONPath examples, see [Usage Guide](usage.md). For troubleshooting symptoms and FAQ, see [Troubleshooting](troubleshooting.md).

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

Help output after installation:

```text
Inspect HorizontalPodAutoscaler status

Usage:
  kubectl-hpa-status [flags]
  kubectl-hpa-status [command]

Available Commands:
  analyze     Analyze one HPA using visible Kubernetes API signals
  completion  Generate shell completion
  doctor      Diagnose HPA scaling failures across metrics, workload, pods, resources, events, and KEDA
  list        List HPAs and highlight visible issues
  scan        Scan all namespaces for HPAs with visible problems
  status      Show concise status for one HPA
  timeline    Show HPA scaling decisions over time
  watch       Watch one HPA status

Common flags include -n/--namespace, -A/--all-namespaces, -o/--output,
--events, --explain, --watch, --interval, --timeout, --since, and --until-condition.
```

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

## Interactive TUI

For the full TUI key bindings and dashboard features, see [Usage Guide - Interactive TUI](usage.md#interactive-tui).

Quick reference:

```sh
kubectl hpa status tui          # Current namespace
kubectl hpa status tui -A       # All namespaces
kubectl hpa status web --watch --dashboard
```

The dashboard auto-refreshes every 5 seconds by default; change the interval with `--interval`. Press `g` to jump to the first HPA with a problem. Press `m` for per-metric diagnostics. Press `space` to select multiple HPAs before entering the CLI batch apply workflow.

## Troubleshooting Patterns

For the complete symptom/command table and FAQ, see [Troubleshooting](troubleshooting.md).

## Health Score Details

For the full health score deduction table and health state definitions, see [Usage Guide - Health Score](usage.md#health-score).

## Compatibility Matrix

Kubernetes v1.26 through v1.36 is the verified support range. This plugin uses `autoscaling/v2`, which went GA in Kubernetes 1.23 and has been a stable API since 1.26. It is expected to work on future Kubernetes versions as long as `autoscaling/v2` is available.

| Environment | Status |
| --- | --- |
| HPA API `autoscaling/v2` | Required |
| Kubernetes v1.26 - v1.36 | Verified and supported |
| metrics-server v0.8.1 on kind | Verified |
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
E2E runs on a matrix of Kubernetes 1.26 / 1.28 / 1.30 / latest-tracking kind image to continuously verify `autoscaling/v2` compatibility across the supported range.

## Roadmap

- [x] **Integration testing:** `kind`-based E2E tests for CI verification.
- [x] **Demo visuals:** Screenshots added to documentation.
- [x] **Homebrew distribution:** GoReleaser generates Homebrew Cask and SBOM for the dedicated Tap.
- [x] **Interactive TUI monitor:** Extended watch mode into a rich terminal dashboard.
- [x] **Batch analysis:** `scan` / `list -A --problem` for bulk diagnosis of problematic HPAs.
- [x] **Selector / multiple HPA targets:** `--selector` on `list` / `scan` and `status hpa-a hpa-b` support.
- [x] **Suggest/Fix feature:** `--suggest` / `--fix --apply` shows concrete patch suggestions and an apply flow.
- [x] **KEDA ScaledObject reference:** `--keda` references ScaledObject and shows trigger/condition context.
- [x] **Shell completion:** Generates flag, namespace, context, and HPA name completion for bash/zsh/fish/powershell.
- [x] **Enhanced KEDA integration:** Shows trigger type, metric name, threshold, current value, auth ref, and HPA metric correspondence.
- [x] **Stabilization window countdown:** Shows remaining time and visual progress in TUI and text output.
- [x] **Metrics pipeline diagnostics:** `--diagnose-metrics` shows per-metric health checks and repair hints.
- [x] **Metrics freshness / staleness analyzer:** `--metrics-freshness` checks HPA currentMetrics, FailedGet*Metric Events, metrics API discovery, PodMetrics timestamps/windows, and KEDA trigger context.
- [x] **Resource consistency check:** `--check-resources` verifies HPA target vs pod resource requests/limits.
- [x] **Doctor command:** `doctor <name>` bundles metrics, workload, pod, resource, event, capacity, and KEDA diagnostics for incident triage.
- [x] **Report output:** `--report markdown` / `--report html` generates single and list diagnostic reports.
- [x] **TUI multi-select:** TUI supports `space` / `a` / `A` for multi-select and CLI batch apply guidance for selected HPAs.
- [x] **Multi-Metric Decision Deep Trace:** Per-metric analysis with tolerance/stabilization impact.
- [x] **What-If Scaling Simulator:** `--simulate-metric` to preview metric value changes.
- [x] **Best Practice Auditor:** `recommend` subcommand for HPA configuration audit with compliance scoring.
- [x] **Retrospective Scaling Timeline:** `timeline --since=30m` reconstructs past scaling decisions from Kubernetes events.
- [ ] **TUI batch apply workflow:** Add in-TUI suggest and safe-confirmed apply for multiple HPAs, equivalent to CLI `list --problem --fix --apply`.
- [ ] **Custom / External Metrics deep dive:** Extend beyond API discovery and HPA-visible freshness signals to add adapter-specific estimation and Prometheus/custom metrics verification hints.
- [ ] **Report summary enhancement:** Add cluster-wide summary, bottom-N health scores, and recommended actions list.
- [ ] **Informer-based watch:** Maintain current polling while adding opt-in informer updates for large-scale clusters.
- [ ] **KEP-6111 structured decision adapter:** Maintain a small adapter boundary to convert future structured HPA decision fields into existing Analysis.
- [ ] **Supply-chain hardening:** Add SLSA provenance and cosign signing to GoReleaser for enterprise verification.

## Demo Recordings

- Screenshot: [images/demo.png](../images/demo.png)
- Comparison image: [images/describe-vs-hpa-status.svg](../images/describe-vs-hpa-status.svg)
- status explain demo: [docs/status-explain.cast](status-explain.cast)
- wide list demo: [docs/list-wide.cast](list-wide.cast)
- watch demo: [docs/watch.cast](watch.cast)
- `--explain` to `--suggest` and `--fix --apply` flow: [docs/fix-flow.cast](fix-flow.cast)
- Zenn article draft: [docs/zenn-hpa-status-ja.md](zenn-hpa-status-ja.md)

Social preview source file: [images/social-preview.svg](../images/social-preview.svg)
