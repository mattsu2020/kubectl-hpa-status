# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![CodeQL](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml)
[![Release](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mattsu2020/kubectl-hpa-status.svg)](https://pkg.go.dev/github.com/mattsu2020/kubectl-hpa-status)
[![Go Report Card](https://goreportcard.com/badge/github.com/mattsu2020/kubectl-hpa-status)](https://goreportcard.com/report/github.com/mattsu2020/kubectl-hpa-status)
[![Stars](https://img.shields.io/github/stars/mattsu2020/kubectl-hpa-status?style=social)](https://github.com/mattsu2020/kubectl-hpa-status/stargazers)
[![Release](https://img.shields.io/github/v/release/mattsu2020/kubectl-hpa-status)](https://github.com/mattsu2020/kubectl-hpa-status/releases)
[![GoReleaser](https://img.shields.io/badge/release-GoReleaser-00add8)](https://goreleaser.com/)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-blue)](https://golangci-lint.run/)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![Kubernetes](https://img.shields.io/badge/kubernetes-autoscaling%2Fv2-326ce5)](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
[![Codecov](https://codecov.io/gh/mattsu2020/kubectl-hpa-status/branch/main/graph/badge.svg)](https://codecov.io/gh/mattsu2020/kubectl-hpa-status)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

![kubectl-hpa-status demo](images/demo.png)

A kubectl plugin for investigating HorizontalPodAutoscaler (HPA) status using existing Kubernetes API signals, with detailed scaling analysis.

Japanese README: [README.ja.md](README.ja.md)

Doc sync note: `README.md` is the primary source at release time. When changing user-facing flags, install steps, or examples, sync `README.ja.md` as well.

This tool quickly answers three common HPA operations questions:

- Is this HPA healthy, capped at limits, in stabilization, or failing to read metrics?
- Which condition or metric best explains the current behavior?
- What command should I run next, and can I safely dry-run it?

## Before / After

<table>
<tr>
<th>Before: raw <code>kubectl describe hpa</code></th>
<th>After: <code>kubectl hpa status --explain</code></th>
</tr>
<tr>
<td>
<pre><code>Name: web
Namespace: production
Metrics: cpu: 92% / 60%
Min replicas: 2
Max replicas: 10
Deployment pods: 10 current / 10 desired
Conditions:
  AbleToScale=True
  ScalingActive=True
  ScalingLimited=True
Events:
  SuccessfulRescale New size: 10</code></pre>
</td>
<td>
<pre><code>web production
Summary: limited at maxReplicas
Replicas: 10 current / 10 desired
CPU: 92% / 60% target

Interpretation:
- HPA wants more replicas, but maxReplicas=10 caps it.
- ScalingActive=True, so metrics are available.

Recommended actions:
- Check capacity, then raise maxReplicas with --suggest.</code></pre>
</td>
</tr>
</table>

The repository name and binary name are `kubectl-hpa-status`. `kubehpa_cli` is an early development directory name/nickname and is not used in release artifacts, Go module path, or install commands.

## Demo

- Screenshot: [images/demo.png](images/demo.png)
- Comparison image: [images/describe-vs-hpa-status.svg](images/describe-vs-hpa-status.svg)
- status explain demo: [docs/status-explain.cast](docs/status-explain.cast)
- wide list demo: [docs/list-wide.cast](docs/list-wide.cast)
- watch demo: [docs/watch.cast](docs/watch.cast)
- `--explain` to `--suggest` and `--fix --apply` flow: [docs/fix-flow.cast](docs/fix-flow.cast)
- Zenn article draft: [docs/zenn-hpa-status-ja.md](docs/zenn-hpa-status-ja.md)

![kubectl describe hpa versus kubectl-hpa-status](images/describe-vs-hpa-status.svg)

| Workflow | Visual |
| --- | --- |
| `status --explain` | [status-explain.svg](images/status-explain.svg) |
| `list -A --wide --problem` | [list-wide.svg](images/list-wide.svg) |
| `watch --interval 5s` | [watch-mode.svg](images/watch-mode.svg) |
| `--suggest` dry-run command | [suggest-dry-run.svg](images/suggest-dry-run.svg) |
| `--fix --apply` diff prompt | [apply-diff.svg](images/apply-diff.svg) |
| Japanese labels (`--lang=ja`) | [ja-output.svg](images/ja-output.svg) |
| `scan` cluster triage | [scan-output.svg](images/scan-output.svg) |
| JSON output | [json-output.svg](images/json-output.svg) |
| Metrics failure | [metrics-failure.svg](images/metrics-failure.svg) |
| Scale-down stabilization | [stabilized-output.svg](images/stabilized-output.svg) |
| Multi-metric estimation | [multi-metric-output.svg](images/multi-metric-output.svg) |

Social preview source file: [images/social-preview.svg](images/social-preview.svg)

### Why use `kubectl-hpa-status`?

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

## Quick start

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status <hpa-name> --explain
kubectl hpa status <hpa-name> --suggest
kubectl hpa status <hpa-name> --fix --apply
kubectl hpa status <hpa-name> --fix --apply --dry-run=false
kubectl hpa status <hpa-name> --lang=ja
kubectl hpa status <hpa-name> --debug
kubectl hpa status hpa-a hpa-b -n production
kubectl hpa status scan
kubectl hpa status list -A --problem
kubectl hpa status list -A --wide --sort-by=desired --filter=scaling-limited
kubectl hpa status list -A --selector='app=web,tier!=canary'
kubectl hpa status ls -A -o json
kubectl hpa status scan --apply --yes
kubectl hpa status <hpa-name> --watch --timeout=2m --until-condition=scaling-limited
kubectl hpa status <hpa-name> -o 'jsonpath={.analysis.summary}'
```

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
  list        List HPAs and highlight visible issues
  scan        Scan all namespaces for HPAs with visible problems
  status      Show concise status for one HPA
  watch       Watch one HPA status

Common flags include -n/--namespace, -A/--all-namespaces, -o/--output,
--events, --explain, --watch, --interval, --timeout, and --until-condition.
```

## Install

### Krew (recommended)

```sh
# Install via Krew (after publishing to the official krew-index)
kubectl krew install hpa-status

# Before publishing, install directly from the manifest:
kubectl krew install --manifest https://raw.githubusercontent.com/mattsu2020/kubectl-hpa-status/main/.krew.yaml
```

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status list -A --wide
kubectl hpa status <hpa-name> --suggest
```

With Krew, the plugin is registered as `hpa-status`. kubectl discovers hyphenated
plugins via the underscore form `kubectl hpa_status`.
**Important: When installed via Krew, you typically use `kubectl hpa_status status <name>`
instead of `kubectl hpa status <name>`.** This README uses `kubectl hpa status` as the
recommended form for environments where kubectl's nested plugin discovery is supported.
If it does not work, use `kubectl hpa_status status <hpa-name>` or
`kubectl-hpa-status status <hpa-name>`.**

### Homebrew

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

The Homebrew Cask automates updates to the dedicated Tap via `homebrew_casks` in `.goreleaser.yml`. Run `make release-check` before release to verify the GoReleaser configuration.

### Manual install

```sh
go mod tidy
go build -o kubectl-hpa-status .
chmod +x ./kubectl-hpa-status
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl hpa status <hpa-name> -n <namespace>
```

For read-only RBAC and the patch permissions needed for `--apply --dry-run=false`,
see [docs/rbac.yaml](docs/rbac.yaml).

Minimum read permissions:

- `get`, `list`, `watch` on `autoscaling/v2` `horizontalpodautoscalers`
- `list`, `watch` on core `events`
- `get` on `deployments`, `statefulsets`, `replicasets` for supplementary not-ready replica display (optional)
- `get`, `list` on KEDA `scaledobjects` when using `--keda` (optional)

Normal diagnostics require no write permissions. Only grant HPA `patch` for operations that explicitly use `--apply --dry-run=false`.

The Go module path, GitHub repository, release metadata, and user-facing binary name are all unified under `github.com/mattsu2020/kubectl-hpa-status` / `kubectl-hpa-status`.

### Requirements

- **Kubernetes 1.26+** (`autoscaling/v2` went GA in 1.23, stable API in 1.26)
- kubectl configured with a kubeconfig
- metrics-server (for CPU/memory metrics) or a custom/external metrics adapter

## Examples

Practical sample manifests are in [examples/](examples/).

| Example | Content |
| --- | --- |
| [cpu-memory-hpa.yaml](examples/cpu-memory-hpa.yaml) | CPU + Memory multi-metric HPA |
| [behavior-hpa.yaml](examples/behavior-hpa.yaml) | scaleUp/scaleDown policies and stabilization window |
| [custom-metrics-hpa.yaml](examples/custom-metrics-hpa.yaml) | Object metric example for custom metrics adapter |
| [keda-style-hpa.yaml](examples/keda-style-hpa.yaml) | KEDA-style labels with External metric HPA |

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
kubectl delete namespace hpa-status-examples
```

## Usage

```sh
kubectl hpa status <hpa-name> [<hpa-name>...] [-n namespace] [--context context] [--events=false]
kubectl hpa status <hpa-name> --watch --interval 5s
kubectl hpa status <hpa-name> --watch --dashboard
kubectl hpa status <hpa-name> --watch --timeout 2m --until-condition scaling-limited
kubectl hpa status analyze <hpa-name> [<hpa-name>...]  # deprecated; use status --explain instead
kubectl hpa status list [-A] [--selector app=web] [--sort-by health-score] [--min-score 60] [--filter scaling-limited]
kubectl hpa status list -A --problem
kubectl hpa status scan --selector app=web
kubectl hpa status ls [-A] --wide
kubectl hpa status watch <hpa-name> --interval 5s
kubectl hpa-status __complete status ""
```

You can also run the binary directly:

```sh
kubectl-hpa-status analyze <hpa-name> -n <namespace>
kubectl-hpa-status status <hpa-name> -n <namespace>
kubectl-hpa-status status <hpa-name> --suggest
kubectl-hpa-status status <hpa-name> --fix --apply
kubectl-hpa-status status <hpa-name> --fix --apply --dry-run=false
kubectl-hpa-status scan
kubectl-hpa-status list -A
kubectl-hpa-status completion zsh
kubectl-hpa-status completion powershell
# Diagnose metrics pipeline
kubectl-hpa-status status <hpa-name> --diagnose-metrics
# Check pod resource requests vs HPA target consistency
kubectl-hpa-status status <hpa-name> --check-resources
# Generate Markdown report
kubectl-hpa-status status <hpa-name> --report markdown
```

Detailed flags:

| Flag | Applies to | Description |
| --- | --- | --- |
| `-n, --namespace` | All commands | Namespace to read when `-A` is not specified. Falls back to kubeconfig namespace, then `default`. |
| `-A, --all-namespaces` | `list`, `scan`, completion | List HPAs across all namespaces. |
| `-l, --selector` | `list`, `scan` | Label selector passed to the HPA list API. Example: `app=web,tier!=canary`. |
| `--context`, `--kubeconfig`, `--cluster` | All commands | kubeconfig selection. |
| `--config <file>` | All commands | Load a YAML/JSON config file. Defaults to `~/.kube/hpa-status.yaml` if it exists. |
| `--chunk-size` | `list`, `scan`, `tui` | Kubernetes list API page size. Default is 500. Set to 0 to disable pagination. |
| `--health-weight name=value` | All analysis commands | Override health score penalties from the CLI. Repeatable. Names: `scalingInactive`, `unableToScale`, `scalingLimited`, `implicitMaxReplicas`, `scaleDownStabilized`, `atMinimumReplicas`. |
| `-o table\|wide\|json\|yaml\|jsonpath=...\|template=...` | status, analyze, list, scan | Output format. Supports YAML output for single and multiple HPAs. |
| `--wide` | table output | Show additional columns including target, min, max, and desired-current diff. |
| `--sort-by namespace\|name\|current\|desired\|diff\|health-score\|issue\|problem` | `list`, `scan` | Sort list output. `problem` prioritizes low scores and replica diffs. |
| `--filter all\|ok\|error\|limited\|scaling-limited\|issue` | `list`, `scan` | Filter by health or issue string. |
| `--health-score`, `--max-score` | `list`, `scan` | Show only HPAs with health score at or below the specified value. |
| `--min-score` | `list`, `scan` | Show only HPAs with health score at or above the specified value. |
| `--problem` | `list`, `scan` | Show only HPAs with visible problems. |
| `--color auto\|always\|never` | text output | Terminal color control. |
| `--interpret` | `status` | Include diagnostic interpretation in compact status output. |
| `--explain` | `status`, `analyze` | Include detailed interpretation and recommended actions. |
| `--suggest`, `--recommend` | `status`, `analyze` | Show safe-looking fixes as `kubectl patch` commands. `--recommend` is an alias for `--suggest`. |
| `--fix` | `status`, `analyze` | Show a stronger fix plan with applicable patches. |
| `--diff` | `status`, `analyze` | Show field-level diff of the proposed HPA spec patch. |
| `--apply` | `status`, `analyze`, `list`, `scan` | Validate HPA patch as server-side dry-run by default. For `list`, combine with `--problem`, `--filter`, or a score filter. |
| `--dry-run=false` | `--apply` flow | Persist changes. Without `-y`, shows a diff and confirmation prompt. |
| `--keda` | `status`, `analyze` | Reference the corresponding ScaledObject for KEDA-managed HPAs, adding trigger/condition context. |
| `--debug`, `-v` | `status`, `analyze`, `list` | Show internal calculations including metric ratio, health score inputs, and condition evidence. |
| `--lang=ja`, `-o ja` | text output | Display labels in Japanese. |
| `--no-interpret` | `status`, `analyze` | Omit interpretation and show only status-derived data. |
| `--events=false` | `status`, `analyze` | Omit recent Events. |
| `--events=3` | `status`, `analyze` | Show the 3 most recent HPA Events. |
| `--diagnose-metrics` | `status`, `analyze` | Show per-metric-type retrieval status, adapter/APIService verification hints, and next troubleshooting steps. |
| `--check-resources` | `status`, `analyze` | Check consistency between HPA target utilization and pod resource requests/limits. |
| `--report markdown\|html` | `status`, `list` | Generate a standalone or cluster-wide diagnostic report in Markdown or HTML. |
| `--watch --interval 5s` | `status`, `watch` | Periodically refresh a single HPA. Watch supports only one HPA name. |
| `--dashboard` | `watch` | Display as a compact terminal dashboard. |
| `--timeout 2m` | watch mode | Stop watching after the specified duration. |
| `--until-condition scaling-limited` | watch mode | Stop watching when the normalized Condition type appears. |
| `--since=30m` | `timeline` | Show retrospective scaling timeline reconstructed from past events. Supports `30m`, `1h`, etc. |
| `--simulate-metric cpu=80%` | `status` | Simulate metric value changes to preview scaling impact. Repeatable. See [What-If Scaling Simulator](#what-if-scaling-simulator). |
| `--version` | root | Show version. |

### Health score

Each HPA is assigned a health score from 0 to 100. The score starts at 100 and penalties are deducted for detected issues:

| Deduction | Score impact |
|-----------|-------------|
| Metrics unavailable (`ScalingActive=False`) | -45 |
| Unable to scale (`AbleToScale!=True`) | -35 |
| Scaling limited by min/maxReplicas | -25 |
| Implicitly reached maxReplicas | -20 |
| Scale-down stabilization window active | -10 |
| Running at minReplicas | -5 |

**Health states**: `OK` -> `STABILIZED` -> `LIMITED` -> `ERROR` (worsening order). Filter by score range using `--health-score`, `--min-score`, and `--max-score`.

Shell completion:

- Generate completion scripts with `kubectl-hpa-status completion bash|zsh|fish|powershell`.
- Supports Cobra's standard `__complete` interface; `kubectl hpa-status __complete status ""` returns HPA name candidates.
- HPA name completion for `status` / `analyze` / `watch` uses the current namespace. With `-A`, candidates are returned in `namespace/name` format.

Configuration file:

When `--config` is omitted, `~/.kube/hpa-status.yaml` is loaded if it exists. Config values serve only as flag defaults; explicit CLI flags always take precedence.

```yaml
namespace: production
lang: en
color: auto
events: 5
selector: app.kubernetes.io/part-of=my-service
sortBy: problem
maxScore: 80
dashboard: false
chunkSize: 500
templates:
  hpa-names:
    type: go-template
    template: '{{ range .Items }}{{ .Namespace }}/{{ .Name }}{{ "\n" }}{{ end }}'
  summaries:
    type: jsonpath
    template: '{.analysis.summary}'
healthWeights:
  scalingInactive: 45
  unableToScale: 35
  scalingLimited: 25
```

Supported Kubernetes versions:

- **Kubernetes 1.26 or later is required.** This plugin uses `autoscaling/v2`, which went GA in Kubernetes 1.23 and has been a stable API since 1.26.
- Runtime target: clusters providing `autoscaling/v2` `HorizontalPodAutoscaler`
- Expected support range: Kubernetes v1.30 through v1.35 `autoscaling/v2`
- Verified cluster: Kubernetes v1.35.0 + metrics-server v0.8.1
- Client libraries: `k8s.io/client-go` / `k8s.io/api` v0.35.0

What this plugin reads:

- `autoscaling/v2` `HorizontalPodAutoscaler`
- `status.currentReplicas`
- `status.desiredReplicas`
- `status.currentMetrics`
- `status.conditions`
- `status.observedGeneration` when present
- `spec.behavior` when present
- Recent HPA Events

This plugin does not reimplement the HPA controller's internal decision logic.

### JSONPath / template output examples

```sh
# List HPA names and health scores
kubectl hpa status list -A -o jsonpath='{range .items[*]}{.namespace}/{.name} {.healthScore}{"\n"}{end}'

# Extract HPAs with health score below 80
kubectl hpa status list -A -o jsonpath='{range .items[?(@.healthScore<80)]}{.namespace}/{.name} {.health}{"\n"}{end}'

# Get KEDA ScaledObject name
kubectl hpa status <hpa> -o jsonpath='{.analysis.keda.scaledObjectName}'

# Get VPA conflict warning
kubectl hpa status <hpa> --vpa -o jsonpath='{.analysis.vpaConflict.warning}'

# Output structured interpretation entries
kubectl hpa status <hpa> -o jsonpath='{range .analysis.structuredInterpretation[*]}{.severity} {.text}{"\n"}{end}'

# Output as JSON for automation
kubectl hpa status list -A -o json | jq '.items[] | {name, namespace, healthScore, issue}'
```

For the full JSON schema, see [docs/output-schema.json](docs/output-schema.json).

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

## Development

```sh
make build
make test
make coverage
make lint
make release-check
```

Design, security, and contribution policies:

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [SECURITY.md](SECURITY.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

E2E testing with kind:

```sh
kind create cluster --name hpa-status-dev
make e2e
kind delete cluster --name hpa-status-dev
```

## Interactive TUI

Launch an interactive dashboard to monitor all HPAs in the cluster in real time:

```sh
kubectl hpa status tui          # Current namespace
kubectl hpa status tui -A       # All namespaces
kubectl hpa status web --watch --dashboard
```

Key bindings:

| Key | Action |
| --- | --- |
| `Up` / `k` | Move cursor up |
| `Down` / `j` | Move cursor down |
| `Enter` | Open HPA detail view |
| `Esc` | Go back / close help |
| `/` | Filter by name, namespace, health state, or issue |
| `S` | Cycle sort order: name -> health-score -> issue -> namespace |
| `g` | Jump to the first HPA with a problem (health != OK) |
| `r` | Refresh data now |
| `p` | Pause / resume auto-refresh |
| `m` | Open per-metric diagnostic detail view |
| `space` | Select / deselect HPA |
| `a` / `A` | Select all visible / deselect all |
| `s` | Show CLI batch apply flow guide for selected HPAs |
| `?` | Toggle key binding help |
| `q` / `Ctrl+c` | Quit |

The dashboard auto-refreshes every 5 seconds by default; change the interval with `--interval`. With `--watch --dashboard` on an interactive terminal, the detail view launches the TUI. For non-interactive output (pipes, recordings), the compact text dashboard is used. Filters accept partial matches across multiple fields. Press `g` to quickly jump to the first HPA that needs attention. Press `m` to check per-metric diagnostics, and `space` to select multiple HPAs before entering the CLI batch apply workflow.

## Troubleshooting patterns

| Symptom | Command | Key signals | Next step |
| --- | --- | --- | --- |
| Not scaling, metrics unavailable | `kubectl hpa status <name> --explain` | `ScalingActive=False`, Events | Check metrics-server or custom/external metrics adapter |
| metrics-server slow or post-restart | `kubectl hpa status <name> --explain --events=10` | Stale `currentMetrics`, `FailedGetResourceMetric`, old `lastTransitionTime` | Wait one scrape interval, check `kubectl top pods` and metrics-server logs |
| Replicas stuck at max | `kubectl hpa status <name> --suggest` | `ScalingLimited=True`, `desiredReplicas == maxReplicas` | Verify capacity, dry-run the proposed maxReplicas patch |
| Scale-down is slow | `kubectl hpa status <name> --explain` | `ScaleDownStabilized`, `spec.behavior.scaleDown` | Wait for stabilization window or adjust it |
| KEDA/External Metrics stop working | `kubectl hpa status <name> --keda --explain --suggest` | KEDA label, External metric, ScaledObject condition, `FailedGetExternalMetric` | Check external metrics API, KEDA ScaledObject, TriggerAuthentication, operator logs, and metric selector |
| Object metric meaning is unclear | `kubectl hpa status <name> --explain` | `Object <kind>/<name>`, ratio | Verify the target Object's value and per-pod load separately |
| HPA wants to scale up but Pods are Pending | `kubectl hpa status <name> --explain` | Pending/Unschedulable Pods | Check node capacity, Cluster Autoscaler/Karpenter events, quotas, affinity, and taints |
| VPA and HPA managing CPU/Memory simultaneously | `kubectl hpa status <name> --vpa --explain` | VPA updateMode, controlled resources, recommendation | Move VPA to recommender-only, or assign CPU/Memory ownership to one side |
| No scaling near tolerance boundary | `kubectl hpa status <name> --explain --debug` | Ratio around 1.02-1.10, desired=current | Check for sustained pressure; consider adjusting HPA tolerance |
| Cluster-wide inventory needed | `kubectl hpa status scan` | health score, issue, conditions | Prioritize `ERROR` items first |
| Summary shows `[STALE STATUS]` | `kubectl hpa status <name> --explain` | `observedGeneration < metadata.generation` | Wait for HPA controller reconciliation; check kube-controller-manager health |
| KEDA-managed HPA with stale external metrics | `kubectl hpa status <name> --keda --explain` | External metric missing from `currentMetrics`, KEDA trigger state | Check `kubectl get scaledobject -n <ns>`, keda-operator Pod logs, and TriggerAuthentication |
| minReplicas=0 cold-start delay | `kubectl hpa status <name> --explain` | `ScaleToZero` shown, no immediate scale-up | Expected behavior; first metric evaluation after scale-to-zero incurs a polling interval delay |
| All metrics show `<unknown>` | `kubectl hpa status <name> --diagnose-metrics` | Per-metric health check, status missing | Check metrics-server, custom metrics adapter registration, and APIService health |
| HPA target utilization differs from expectation | `kubectl hpa status <name> --check-resources` | Resource request warnings, unset requests, target mismatch | Check pod template resource requests. HPA utilization is calculated as usage/request. |
| Incident report needed | `kubectl hpa status <name> --report markdown` | Full single-HPA report | Share to Slack, Notion, or incident management tools |
| Cluster-wide health summary needed | `kubectl hpa status list -A --report markdown` | Cluster-wide report, health scores, top issues | Share for on-call handoff or platform review |
| Batch-fix multiple HPAs | `kubectl hpa status list -A --problem --fix --apply` | All patch summary table | Review the batch summary and apply all at once |

### FAQ

**Can I tell which metric "won" in a multi-metric HPA?** Not with certainty. The plugin estimates from visible `currentMetrics` and `spec.metrics`, but per-metric replica recommendations, missing metric conservative corrections, and the final selection are not exposed by the API.

**What should I run first when I see `Metrics unavailable`?** Start with `kubectl hpa status <name> --explain --diagnose-metrics`. For CPU/memory, check `kubectl top pods` and metrics-server. For custom/external metrics, check the adapter's `APIService`, adapter logs, and metric selector semantics.

**Can I tell if the stabilization window is holding?** Use `kubectl hpa status <name> --explain` or the TUI/watch view. The plugin shows `ScaleDownStabilized` and stabilization window timing to the extent visible from HPA status, behavior policy, and recent events.

**Why doesn't `kubectl hpa status` work after installing via Krew?** Krew publishes hyphenated plugin names via the underscore form. Run `kubectl plugin list` and if you see `hpa-status`, use `kubectl hpa_status status <name>` instead.

**Why does the status show LIMITED even when conditions look healthy?** Some clusters may delay or omit `ScalingLimited` reflection. The plugin also checks explicit replica situations like `current == desired == maxReplicas` and applies a light implicit maxReplicas penalty.

## Compatibility matrix

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

## Verified environments

- kind: v0.31.0
- kind node image: `kindest/node:v1.35.0`
- Kubernetes server: v1.35.0
- kubectl: v1.36.1
- metrics-server: v0.8.1
- HPA API: `autoscaling/v2`

metrics-server was verified using the upstream release manifest with the `--kubelet-insecure-tls` option added for kind.

## Safe fix flow

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

## Limitations

- The Kubernetes HPA API does **not** expose the controller's internal scaling decision trace.
- Multi-metric "winner" detection is a best-effort estimate from visible `currentMetrics`.
- Tolerance, missing-metric dampening, not-ready pod effects, and stabilization's internal recommendation history are not fully exposed in HPA status.
- Events are diagnostic hints, not a durable structured decision log.

## CI/CD

| Workflow | Purpose |
| --- | --- |
| [ci.yml](.github/workflows/ci.yml) | `go test`, coverage, govulncheck, gosec, golangci-lint, kind E2E |
| [codeql.yml](.github/workflows/codeql.yml) | CodeQL static analysis |
| [release.yml](.github/workflows/release.yml) | GoReleaser for binaries, SBOM, Homebrew Cask Tap updates, and Krew release bot |

CI uploads coverage to Codecov. Release Homebrew updates use the dedicated Tap [mattsu2020/homebrew-kubectl-hpa-status](https://github.com/mattsu2020/homebrew-kubectl-hpa-status).
E2E runs on a matrix of Kubernetes 1.26 / 1.28 / 1.30 / latest-tracking kind image to continuously verify `autoscaling/v2` compatibility across the supported range.

## Validation matrix

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

### Retrospective Scaling Timeline

Show estimated past scaling decisions using `timeline --since`:

```bash
kubectl hpa status timeline web -n production --since=30m
```

Output:

```text
HPA Scaling Timeline: web (production)  since 30m ago

10:01 desired 3 -> 5   cpu 142% over target
10:02 scaleUp limited by policy: +2 pods / 60s
10:05 desired 5 -> 5   within tolerance
10:12 desired 5 -> 3   scaleDown suppressed by stabilization window (120s)
10:20 desired 5 -> 3   applied scale down

Note: Best-effort reconstruction from Kubernetes events and current HPA status.
```

Limitations:
- The HPA controller's internal decision history is not fully visible through the Kubernetes API
- Multi-metric winner determination is estimated
- Exact metric values at decision time are not available
- Suppressed scaling decisions that did not produce events may be missing
- Kubernetes events typically expire after ~1 hour, so `--since` values beyond that may return empty results

Supports all output formats: `--since=30m -o json`, `--since=30m -o yaml`, `--since=30m --report markdown`, `--since=30m --report html`.

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
- [x] **Resource consistency check:** `--check-resources` verifies HPA target vs pod resource requests/limits.
- [x] **Report output:** `--report markdown` / `--report html` generates single and list diagnostic reports.
- [x] **TUI multi-select:** TUI supports `space` / `a` / `A` for multi-select and CLI batch apply guidance for selected HPAs.
- [x] **Multi-Metric Decision Deep Trace:** Per-metric analysis with tolerance/stabilization impact.
- [x] **What-If Scaling Simulator:** `--simulate-metric` to preview metric value changes.
- [x] **Best Practice Auditor:** `recommend` subcommand for HPA configuration audit with compliance scoring.
- [x] **Retrospective Scaling Timeline:** `timeline --since=30m` reconstructs past scaling decisions from Kubernetes events.
- [ ] **TUI batch apply workflow:** Add in-TUI suggest and safe-confirmed apply for multiple HPAs, equivalent to CLI `list --problem --fix --apply`.
- [ ] **Custom / External Metrics deep dive:** Extend beyond HPA status visibility to add APIService health, adapter estimation, and Prometheus/custom metrics verification hints.
- [ ] **Report summary enhancement:** Add cluster-wide summary, bottom-N health scores, and recommended actions list.
- [ ] **Informer-based watch:** Maintain current polling while adding opt-in informer updates for large-scale clusters.
- [ ] **KEP-6111 structured decision adapter:** Maintain a small adapter boundary to convert future structured HPA decision fields into existing Analysis.
- [ ] **Supply-chain hardening:** Add SLSA provenance and cosign signing to GoReleaser for enterprise verification.

## License

Apache-2.0
