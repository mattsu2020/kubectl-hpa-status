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

A kubectl plugin for inspecting HorizontalPodAutoscaler status with detailed
scaling analysis using existing Kubernetes API signals.

日本語版README: [README.ja.md](README.ja.md)

Documentation sync note: `README.md` is the release source of truth. Keep
`README.ja.md` in sync for user-facing flags, install paths, and examples when
changing this file.

It answers three common HPA questions quickly:

- Is this HPA healthy, capped, stabilized, or unable to read metrics?
- Which visible metric or condition most likely explains the current behavior?
- What command should I run next, and can I validate it safely first?

The repository and binary are named `kubectl-hpa-status`. The local workspace
name `kubehpa_cli` is only an early development nickname and is not used in
release artifacts, module paths, or install commands.

## Demo

- Screenshot: [images/demo.png](images/demo.png)
- Comparison image: [images/describe-vs-hpa-status.svg](images/describe-vs-hpa-status.svg)
- status explain demo: [docs/status-explain.cast](docs/status-explain.cast)
- wide list demo: [docs/list-wide.cast](docs/list-wide.cast)
- watch demo: [docs/watch.cast](docs/watch.cast)
- explain to suggest to fix flow: [docs/fix-flow.cast](docs/fix-flow.cast)

| Workflow | Visual |
| --- | --- |
| `status --explain` | [status-explain.svg](images/status-explain.svg) |
| `list -A --wide --problem` | [list-wide.svg](images/list-wide.svg) |
| `watch --interval 5s` | [watch-mode.svg](images/watch-mode.svg) |
| `--suggest` dry-run command | [suggest-dry-run.svg](images/suggest-dry-run.svg) |
| `--fix --apply` diff prompt | [apply-diff.svg](images/apply-diff.svg) |
| Japanese labels | [ja-output.svg](images/ja-output.svg) |
| `scan` cluster triage | [scan-output.svg](images/scan-output.svg) |
| JSON output | [json-output.svg](images/json-output.svg) |
| metrics failure | [metrics-failure.svg](images/metrics-failure.svg) |
| scale-down stabilization | [stabilized-output.svg](images/stabilized-output.svg) |
| multi-metric estimate | [multi-metric-output.svg](images/multi-metric-output.svg) |

Social preview source: [images/social-preview.svg](images/social-preview.svg).

```sh
kubectl hpa status list -A --wide
kubectl hpa status <hpa-name> --suggest
kubectl hpa status <hpa-name> --fix --apply
```

![kubectl describe hpa versus kubectl-hpa-status](images/describe-vs-hpa-status.svg)

### Why use `kubectl-hpa-status`?

| Feature | `kubectl describe hpa` | `kubectl hpa status` (This plugin) |
| --- | --- | --- |
| **Focus** | Raw status & spec dumps | Multi-dimensional diagnostics & actions |
| **Scaling Summary** | Standard K8s condition text | Clear operational direction summary |
| **Limitation Detection** | Raw min/max limits displayed | Auto-explains caps when maxReplicas is reached |
| **Multi-Metric Diagnostics** | Lists targets independently | Guesses & highlights the highest impact metric |
| **Stabilization Warning** | Not explicitly tracked | Flags active stabilization windows & suggests wait durations |
| **Watch Mode** | Requires external `watch` (no diff) | Built-in refresh with previous state delta diffs |
| **Recommendation Guide** | None | Explains *why* and suggests config fixes |

## Quick usage

### Which invocation works for you?

kubectl discovers plugins differently depending on your kubectl version and
installation method. After installing, run:

```sh
kubectl plugin list
```

Then use the command path that appears in the output:

| Output from `kubectl plugin list` | Invocation |
| --- | --- |
| `kubectl-hpa-status` (standalone binary in PATH) | `kubectl-hpa-status status <name>` |
| `hpa-status` (Krew symlink) | `kubectl hpa_status status <name>` |
| `hpa/status` (nested plugin directory) | `kubectl hpa status <name>` |

All three invoke the same binary and accept the same flags.
This project documents `kubectl hpa status` as the preferred form.

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status <hpa-name> --explain
kubectl hpa status <hpa-name> --suggest
kubectl hpa status <hpa-name> --fix --apply
kubectl hpa status hpa-a hpa-b -n production
kubectl hpa status list -A --wide --sort-by=desired --filter=scaling-limited
kubectl hpa status list -A --selector='app=web,tier!=canary'
kubectl hpa status ls -A -o json
kubectl hpa status scan --apply --yes
kubectl hpa status <hpa-name> --watch --timeout=2m --until-condition=scaling-limited
kubectl hpa status <hpa-name> -o 'jsonpath={.analysis.summary}'
```

### Config file

If `--config` is omitted and `~/.kube/hpa-status.yaml` exists, the plugin reads
it as default CLI settings. Explicit flags always win over config values.

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

The config file `~/.kube/hpa-status.yaml` is supported and allows setting defaults for all flags. For a complete example with all fields, see [docs/config-example.yaml](docs/config-example.yaml).

How to read the output:

- `Summary` is the visible state derived from HPA status.
- `Recommended actions` are operational hints based on visible conditions and behavior settings.
- `Interpretation` is diagnostic inference, not the controller's private decision trace.
- `confidence: high` means the line is based on explicit status fields; `confidence: medium` means the status is consistent with the explanation but the API does not expose the exact internal reason.
- Multi-metric "winner" lines are intentionally labeled as estimates. Kubernetes HPA status does not expose per-metric replica recommendations today, so the plugin highlights the metric with the largest visible distance from target instead of claiming the exact controller winner.

Common troubleshooting checks:

- `ScalingActive=False`: check metrics-server, custom metrics adapters, or external metrics adapters.
- `ScalingLimited=True`: check `minReplicas`, `maxReplicas`, and target utilization.
- `ScaleDownStabilized`: check `spec.behavior.scaleDown.stabilizationWindowSeconds` and wait for the stabilization window.
- missing or stale output: compare `status.observedGeneration` with `metadata.generation`.

First help output after installation:

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
# Install via Krew (once published to the official krew-index)
kubectl krew install hpa-status

# Until then, install from the local manifest:
kubectl krew install --manifest https://raw.githubusercontent.com/mattsu2020/kubectl-hpa-status/main/.krew.yaml
```

#### Krew official index status

`hpa-status` is not yet in the official krew-index. The current install path
uses a direct manifest URL.

To submit to the official index:

1. Ensure at least one GitHub release with cross-platform binaries.
2. Generate the manifest: `make krew` (validates `.krew.yaml` template).
3. Fork `kubernetes-sigs/krew-index`, copy the resolved manifest to `plugins/hpa-status.yaml`.
4. Open a PR against the krew-index repo following their plugin review process.
5. After merge, `kubectl krew install hpa-status` works for all users.

Track progress: [krew-index plugins](https://github.com/kubernetes-sigs/krew-index/tree/master/plugins)

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status list -A --wide
kubectl hpa status <hpa-name> --suggest
```

Krew installs the plugin as `hpa-status`. For plugins whose names contain
dashes, Krew creates a kubectl-visible symlink using underscores, so
`hpa-status` is discoverable by kubectl as `kubectl hpa_status`.
**Important: Krew users usually need `kubectl hpa_status status <hpa-name>`,
not `kubectl hpa status <hpa-name>`.** This project documents `kubectl hpa status` as the preferred nested command
when your kubectl plugin discovery supports it; if it does not, use
`kubectl hpa_status status <hpa-name>` or the direct binary
`kubectl-hpa-status status <hpa-name>`.**

### Homebrew

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

### Manual

```sh
go mod tidy
go build -o kubectl-hpa-status .
chmod +x ./kubectl-hpa-status
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl hpa status <hpa-name> -n <namespace>
```

To verify the plugin is visible:

```sh
kubectl plugin list
```

Minimum readonly RBAC and optional patch RBAC examples are available in
[docs/rbac.yaml](docs/rbac.yaml). The plugin only needs `patch` permission when
you intentionally use `--apply --dry-run=false`.

Minimum read-only permissions:

- `get`, `list`, `watch` on `autoscaling/v2` `horizontalpodautoscalers`
- `list`, `watch` on core `events`
- optional `get` on `deployments`, `statefulsets`, and `replicasets` for not-ready target replica hints
- optional `get`, `list` on KEDA `scaledobjects` when using `--keda`

Write permission is not required for normal diagnostics. Add `patch` on HPAs
only for the explicit `--apply --dry-run=false` workflow.

The Go module path, GitHub repository, release metadata, and user-facing binary
name now all use `github.com/mattsu2020/kubectl-hpa-status` /
`kubectl-hpa-status`.

### Requirements

- **Kubernetes v1.26 through v1.36** (tested range). The plugin uses `autoscaling/v2`
  which became GA in Kubernetes 1.23 and is the stable API from 1.26 onward.
  The plugin is expected to work with future Kubernetes versions that serve
  `autoscaling/v2`; the tested range will be updated as CI validation expands.
- kubectl configured with a kubeconfig
- metrics-server (for CPU/memory metrics) or custom/external metrics adapter

## Examples

Practical manifests live in [examples/](examples/):

| Example | What it demonstrates |
| --- | --- |
| [cpu-memory-hpa.yaml](examples/cpu-memory-hpa.yaml) | CPU and memory HPA for multi-metric diagnostics |
| [behavior-hpa.yaml](examples/behavior-hpa.yaml) | scaleUp/scaleDown policies and stabilization windows |
| [custom-metrics-hpa.yaml](examples/custom-metrics-hpa.yaml) | object metric shape for custom metrics adapters |
| [keda-style-hpa.yaml](examples/keda-style-hpa.yaml) | KEDA-style HPA labels and external metrics |

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
# Diagnose metrics pipeline issues
kubectl hpa status web-multi -n hpa-status-examples --diagnose-metrics
# Check resource request consistency
kubectl hpa status web-multi -n hpa-status-examples --check-resources
# Generate a standalone report
kubectl hpa status web-multi -n hpa-status-examples --report markdown
kubectl delete namespace hpa-status-examples
```

## Usage

```sh
kubectl hpa status <hpa-name> [<hpa-name>...] [-n namespace] [--context context] [--events=false]
kubectl hpa status <hpa-name> --watch --interval 5s
kubectl hpa status <hpa-name> --watch --timeout 2m --until-condition scaling-limited
kubectl hpa status analyze <hpa-name> [<hpa-name>...]  # deprecated; use `status --explain` instead
kubectl hpa status list [-A] [--selector app=web] [--sort-by desired] [--filter scaling-limited]
kubectl hpa status list -A --problem
kubectl hpa status scan --selector app=web
kubectl hpa status ls [-A] --wide
kubectl hpa status watch <hpa-name> --interval 5s
```

The released binary name is `kubectl-hpa-status`. Krew links it as a kubectl
plugin named `hpa-status`. Plugin command parsing can vary by kubectl version,
so validate the exact invocation with:

```sh
kubectl plugin list
```

Direct binary usage is also supported:

```sh
kubectl-hpa-status analyze <hpa-name> -n <namespace>
kubectl-hpa-status status <hpa-name> -n <namespace>
kubectl-hpa-status status <hpa-name> --suggest
kubectl-hpa-status status <hpa-name> --fix --apply
kubectl-hpa-status status <hpa-name> --fix --apply --dry-run=false
kubectl-hpa-status scan
kubectl-hpa-status list -A
kubectl-hpa-status completion zsh
```

`analyze` is the detailed diagnostic command. `status` is intentionally more
compact by default; pass `--interpret` when you want interpretation in status
output.

Detailed flags:

| Flag | Applies to | Description |
| --- | --- | --- |
| `-n, --namespace` | all commands | Namespace to read when `-A` is not set. Defaults to the current kubeconfig namespace or `default`. |
| `-A, --all-namespaces` | `list`, `scan`, completion | List HPAs across all namespaces. |
| `-l, --selector` | `list`, `scan` | Kubernetes label selector passed to the HPA list call, such as `app=web,tier!=canary`. |
| `--context`, `--kubeconfig`, `--cluster` | all commands | Explicit kubeconfig selection. |
| `--config` | all commands | Read defaults from a YAML/JSON config file. Defaults to `~/.kube/hpa-status.yaml` when present. |
| `--chunk-size` | `list`, `scan`, `tui` | Kubernetes list page size. Defaults to 500; set 0 to disable pagination. |
| `--health-weight name=value` | all analysis commands | Override one health score penalty from the CLI. Repeatable; names include `scalingInactive`, `unableToScale`, `scalingLimited`, `implicitMaxReplicas`, `scaleDownStabilized`, and `atMinimumReplicas`. |
| `-o table|wide|json|yaml|jsonpath=...|template=...` | status, analyze, list, scan | Output format. YAML is supported for both single and multiple HPA output. For the JSON schema, see [docs/output-schema.json](docs/output-schema.json). |
| `--wide` | table output | Show target, min, max, and replica delta columns where applicable. |
| `--sort-by namespace|name|current|desired|diff|health-score|issue|problem` | `list`, `scan` | Sort list output. `problem` puts the lowest health score and largest replica delta first. |
| `--filter all|ok|error|limited|scaling-limited|issue` | `list`, `scan` | Filter by health or issue text. |
| `--health-score`, `--max-score` | `list`, `scan` | Show only HPAs whose health score is at or below the threshold. |
| `--min-score` | `list`, `scan` | Show only HPAs whose health score is at or above the threshold. |
| `--problem` | `list`, `scan` | Show only HPAs with a visible issue. |
| `--color auto|always|never` | text output | Control terminal color output. |
| `--interpret` | `status` | Include diagnostic interpretation in compact status output. |
| `--explain` | `status`, `analyze` | Include detailed interpretation and recommended actions. |
| `--suggest`, `--recommend` | `status`, `analyze` | Include concrete `kubectl patch` commands when a safe HPA spec suggestion is visible. `--recommend` is an alias for `--suggest`. |
| `--fix` | `status`, `analyze` | Show a stronger fix plan with applicable patches. |
| `--diff` | `status`, `analyze` | Include field-level diffs for suggested HPA spec patches. |
| `--apply` | `status`, `analyze`, `list`, `scan` | Validate suggested HPA patches with server-side dry-run by default. For `list`, combine it with `--problem`, `--filter`, or a score filter. |
| `--dry-run=false` | `--apply` workflow | Persist changes; still shows a diff and asks for confirmation unless `-y` is set. |
| `--keda` | `status`, `analyze` | For KEDA-managed HPAs, look up the matching ScaledObject and include trigger details (metric name, threshold, current value, auth ref). |
| `--vpa` | `status`, `analyze` | Detect VerticalPodAutoscaler conflicts with the HPA target. |
| `--diagnose-metrics` | `status`, `analyze` | Run comprehensive metrics pipeline health checks with per-metric status and remediation steps. |
| `--check-resources` | `status`, `analyze` | Validate HPA target utilization against pod resource requests/limits. |
| `--report markdown\|html` | `status`, `list` | Generate standalone reports in Markdown or HTML format. |
| `--lang=ja`, `-o ja` | text output | Show Japanese text labels. |
| `--no-interpret` | `status`, `analyze` | Omit interpretation and show status-derived data only. |
| `--events=false` | `status`, `analyze` | Omit recent HPA Events. |
| `--events=3` | `status`, `analyze` | Show the latest 3 HPA Events. |
| `--watch --interval 5s` | `status`, `watch` | Refresh one HPA periodically. Watch mode accepts exactly one HPA name. |
| `--dashboard` | `watch` | Render watch output as a compact terminal dashboard. |
| `--timeout 2m` | watch mode | Stop watch after a duration. |
| `--until-condition scaling-limited` | watch mode | Stop watch once the normalized condition type is present. |
| `--version` | root | Print the plugin version. |

### Health Score

Each HPA receives a health score from 0 to 100. The score starts at 100 and penalties are deducted for detected issues:

| Deduction | Score Impact |
|-----------|-------------|
| Metrics unavailable (`ScalingActive=False`) | -45 |
| Unable to scale (`AbleToScale!=True`) | -35 |
| Scaling limited by min/maxReplicas | -25 |
| Implicitly at maxReplicas | -20 |
| Scale-down stabilization window active | -10 |
| At minimum replicas | -5 |

**Health states**: `OK` → `STABILIZED` → `LIMITED` → `ERROR` (worsening order). Use `--health-score`, `--min-score`, and `--max-score` to filter by score range.

Supported Kubernetes versions:

- **Kubernetes 1.26 or later is required.** The plugin uses `autoscaling/v2` which became GA in Kubernetes 1.23 and is the stable API from 1.26 onward.
- Runtime target: clusters serving `autoscaling/v2` `HorizontalPodAutoscaler`
- Validated cluster: Kubernetes v1.35.0 with metrics-server v0.8.1
- Client libraries: `k8s.io/client-go` / `k8s.io/api` v0.35.0

The plugin reads:

- `autoscaling/v2` `HorizontalPodAutoscaler`
- `status.currentReplicas`
- `status.desiredReplicas`
- `status.currentMetrics`
- `status.conditions`
- `status.observedGeneration`, when present
- `spec.behavior`, when present
- recent HPA Events

It intentionally does not reimplement the HPA controller's internal decision logic.

### JSONPath and template output examples

```sh
# List HPA names with health scores
kubectl hpa status list -A -o jsonpath='{range .items[*]}{.namespace}/{.name} {.healthScore}{"\n"}{end}'

# Show only HPAs with health score below 80
kubectl hpa status list -A -o jsonpath='{range .items[?(@.healthScore<80)]}{.namespace}/{.name} {.health}{"\n"}{end}'

# Extract KEDA ScaledObject name for KEDA-managed HPAs
kubectl hpa status status <hpa> -o jsonpath='{.analysis.keda.scaledObjectName}'

# Get VPA conflict warning
kubectl hpa status status <hpa> --vpa -o jsonpath='{.analysis.vpaConflict.warning}'

# Get structured interpretation entries
kubectl hpa status status <hpa> -o jsonpath='{range .analysis.structuredInterpretation[*]}{.severity} {.text}{"\n"}{end}'

# Output per-HPA summary as JSON for automation
kubectl hpa status list -A -o json | jq '.items[] | {name, namespace, healthScore, issue}'
```

For the full JSON schema, see [docs/output-schema.json](docs/output-schema.json).

## Validated environment

- kind: v0.31.0
- kind node image: `kindest/node:v1.35.0`
- Kubernetes server: v1.35.0
- kubectl: v1.36.1
- metrics-server: v0.8.1
- HPA API: `autoscaling/v2`

metrics-server was installed from the upstream release manifest with the
kind-specific `--kubelet-insecure-tls` option.

## Interactive TUI

Launch a real-time interactive dashboard for monitoring HPAs across the cluster:

```sh
kubectl hpa status tui          # current namespace
kubectl hpa status tui -A       # all namespaces
```

Key bindings:

| Key | Action |
| --- | --- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Enter` | Open HPA detail view |
| `Esc` | Go back / Close help |
| `/` | Filter by name, namespace, health status, or issue text |
| `S` | Cycle sort: name → health-score → issue → namespace |
| `g` | Jump to first problematic HPA (health ≠ OK) |
| `m` | View per-metric diagnostics detail |
| `space` | Toggle HPA selection for batch operations |
| `a` | Select all filtered HPAs |
| `A` | Deselect all |
| `s` | Show apply hint for selected HPAs |
| `r` | Refresh data now |
| `p` | Pause / resume auto-refresh |
| `?` | Toggle key binding help overlay |
| `q` / `Ctrl+c` | Quit |

The dashboard auto-refreshes every 5 seconds. Filter accepts partial matches across multiple fields. Sort cycles through available columns. Use `g` to quickly jump to the first HPA that needs attention. Press `m` to view per-metric diagnostics, or use `space` to select HPAs for batch operations.

## Troubleshooting patterns

| Symptom | Command | Primary signals | Likely next step |
| --- | --- | --- | --- |
| HPA is not scaling and metrics are missing | `kubectl hpa status <name> --explain` | `ScalingActive=False`, Events | Check metrics-server or custom/external metrics adapters |
| metrics-server is slow or recently restarted | `kubectl hpa status <name> --explain --events=10` | stale `currentMetrics`, `FailedGetResourceMetric`, old `lastTransitionTime` | Wait for the scrape interval, then check `kubectl top pods` and metrics-server logs |
| Replicas are capped at the top | `kubectl hpa status <name> --suggest` | `ScalingLimited=True`, `desiredReplicas == maxReplicas` | Review capacity, then validate the suggested maxReplicas patch |
| Scale-down looks delayed | `kubectl hpa status <name> --explain` | `AbleToScale=True`, `ScaleDownStabilized`, `spec.behavior.scaleDown` | Wait for or tune stabilization window |
| KEDA-managed HPA is not reacting | `kubectl hpa status <name> --keda --explain` | KEDA labels, External metrics, ScaledObject conditions | Check ScaledObject triggers, TriggerAuthentication, external metrics API, and KEDA operator logs |
| Custom or external metric looks ambiguous | `kubectl hpa status <name> --explain --debug` | External/Object metric ratio, missing current status | Confirm adapter freshness and metric selector semantics outside the HPA status API |
| HPA wants to scale up but pods stay Pending | `kubectl hpa status <name> --explain` | Pending/Unschedulable target pods | Check node capacity, Cluster Autoscaler/Karpenter events, quotas, affinity, and taints |
| VPA and HPA both manage CPU/memory | `kubectl hpa status <name> --vpa --explain` | VPA updateMode, controlled resources, recommendations | Prefer VPA recommender-only mode or avoid overlapping CPU/memory ownership |
| Many HPAs need triage | `kubectl hpa status scan` | Health score, issue, conditions | Start with `ERROR`, then `ScalingLimited` |
| `[STALE STATUS]` prefix in summary | `kubectl hpa status <name> --explain` | `observedGeneration < metadata.generation` | Wait for HPA controller reconciliation; check kube-controller-manager health |
| KEDA external metric stale on managed HPA | `kubectl hpa status <name> --keda --explain` | Missing external metric in `currentMetrics`, KEDA trigger status | Verify `kubectl get scaledobject -n <ns>`, check keda-operator pod logs, verify TriggerAuthentication |
| minReplicas=0 cold start delay | `kubectl hpa status <name> --explain` | `ScaleToZero` indicator, no immediate scale-up | Expected behavior; first metric evaluation after scale-to-zero introduces a delay equal to the polling interval |
| All metrics show `<unknown>` | `kubectl hpa status <name> --diagnose-metrics` | Per-metric health checks, missing status | Check metrics-server deployment, custom metrics adapter registration, API service health |
| HPA target utilization seems wrong | `kubectl hpa status <name> --check-resources` | Resource request warnings, zero requests, target mismatch | Review pod template resource requests; HPA utilization = usage / request |
| Need an incident report | `kubectl hpa status <name> --report markdown` | Standalone report with all sections | Paste into Slack, Notion, or incident tracking tool |
| Batch fix multiple HPAs | `kubectl hpa status list -A --problem --fix --apply` | Summary table of all patches | Review the batch summary, confirm once to apply all |

### FAQ

**Which metric won in a multi-metric HPA?** The plugin can only estimate from
visible `currentMetrics` and `spec.metrics`. It cannot see the controller's
per-metric replica recommendations, missing-metric dampening, or final
selection before min/max and stabilization constraints.

**Why does `kubectl hpa status` fail after Krew install?** Krew exposes
dash-separated plugin names through underscores. Run `kubectl plugin list`; if
you see `hpa-status`, use `kubectl hpa_status status <name>`.

**Why does the score say LIMITED when conditions look healthy?** Some clusters
lag or omit `ScalingLimited`; the plugin also checks explicit replica evidence
such as `current == desired == maxReplicas` and applies the smaller implicit
maxReplicas penalty.

## Compatibility matrix

Kubernetes v1.26 through v1.36 is the tested range. The plugin uses `autoscaling/v2` which became GA in Kubernetes 1.23 and is the stable API from 1.26 onward. The plugin is expected to work with future Kubernetes versions that serve `autoscaling/v2`.

| Environment | Status |
| --- | --- |
| HPA API `autoscaling/v2` | Required |
| Kubernetes v1.26 - v1.36 | Tested and supported |
| metrics-server v0.8.1 on kind | Validated |
| custom/external metrics adapters | Supported through visible HPA status with best-effort ratio and selector interpretation; adapter-specific internals are not inspected |
| KEDA 2.0+ (`keda.sh/v1alpha1`) | Automatic KEDA-managed HPA detection. `--keda` looks up the ScaledObject for trigger details (type, metric name, threshold, current value, auth ref), polling interval, cooldown, and fallback configuration |
| VPA 0.9+ (`autoscaling.k8s.io/v1`) | `--vpa` detects same-target CPU/memory overlap and includes visible VPA recommendations when the VPA CRD is installed |
| Shell Completion | bash, zsh, fish, PowerShell with dynamic HPA name, namespace, and context completion |

## Safe fix workflow

Suggestions are intentionally conservative:

1. `--suggest` prints copy-pasteable `kubectl patch` commands with `--dry-run=server`.
2. `--fix --apply` still defaults to server-side dry-run and prints a field-level diff before asking for confirmation.
3. Persisting changes requires `--dry-run=false`; this is never the default.
4. maxReplicas suggestions include preconditions and warnings because raising a ceiling can affect node capacity, quotas, cost, and downstream systems.
5. The preview explains the expected effect, such as allowing immediate scale-up if metrics still require more replicas.

Dry-run modes:

- `--dry-run=server` asks the Kubernetes API server to validate the patch with admission and defaulting, but it does not persist the change.
- `--dry-run=client` only validates locally in kubectl and may miss server-side admission behavior.
- `kubectl-hpa-status --apply` uses server-side dry-run by default. Persistent changes require `--dry-run=false`.

## Limitations

- The Kubernetes HPA API does not expose the controller's exact internal scaling decision trace.
- Multi-metric "winner" detection is a best-effort impact estimate from visible `currentMetrics` and `spec.metrics`.
- Tolerance, conservative handling of missing metrics, not-ready pods, and stabilization recommendation history are not fully exposed in HPA status.
- Events are useful recent context, but they are not treated as a durable structured decision log.

## CI/CD

| Workflow | Purpose |
| --- | --- |
| [ci.yml](.github/workflows/ci.yml) | `go test`, coverage, govulncheck, gosec, golangci-lint, and kind E2E |
| [codeql.yml](.github/workflows/codeql.yml) | CodeQL static analysis |
| [release.yml](.github/workflows/release.yml) | GoReleaser binaries, SBOM, Homebrew Cask tap update, and Krew release bot |

Coverage is uploaded to Codecov when CI runs. Release automation uses the
dedicated Homebrew tap
[mattsu2020/homebrew-kubectl-hpa-status](https://github.com/mattsu2020/homebrew-kubectl-hpa-status).
The E2E job runs a Kubernetes version matrix covering 1.26, 1.28, 1.30, and the
current tracked kind image so regressions across supported `autoscaling/v2`
clusters are caught early.

## Validation matrix

| Case | Explainable with existing signals? | Signals used | Remaining ambiguity |
| --- | --- | --- | --- |
| CPU above target and scale up | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Low |
| CPU below target and scale down | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Low |
| Limited by `maxReplicas` | Yes | `ScalingLimited`, `maxReplicas` | Low |
| Metrics fetch failure | Yes | `ScalingActive=False`, Events | Low |
| Multiple metrics and final winner | Partially hard | `currentMetrics`, `spec.metrics` | Medium |
| Scale-down stabilization | Partially yes | `AbleToScale`, condition reason, Events | Medium |
| Tolerance-based no-scale | Hard | `currentMetrics`, `desiredReplicas` | Medium to high |
| Missing metrics or not-ready pods affect decision | Hard | insufficient existing status | High |

Events are useful as recent diagnostic context, but this POC does not treat
them as a stable decision record.

## Local validation notes

Validated on a local kind cluster named `hpa-status-poc`.

| Case | Observed result |
| --- | --- |
| metrics-server absent | `ScalingActive=False`, `FailedGetResourceMetric`, and HPA Events explained the metric fetch failure. `desiredReplicas=0` was not treated as a scale-down recommendation. |
| metrics-server present, CPU below target | `ScalingActive=True`, `currentMetrics` showed CPU below target, and desired replicas stayed unchanged. |
| CPU above target | HPA emitted `SuccessfulRescale` and scaled the deployment upward. |
| maxReplicas limit | `ScalingLimited=True`, `TooManyReplicas`, and `desiredReplicas == maxReplicas` were enough to explain the visible cap. |
| scale-down stabilization | After load stopped, CPU dropped below target while `AbleToScale=True` with `ScaleDownStabilized`; the plugin could explain the visible condition but not the controller's internal recommendation history. |
| multi-metric HPA with maxReplicas cap | CPU and memory both appeared in `currentMetrics`. The visible desired replica count was capped by `maxReplicas`, so the POC could not reliably distinguish the selected metric recommendation from the limiting behavior. |
| tolerance-like no-scale | Memory was slightly above target (`73%/70%`, ratio approximately `1.043`) while `currentReplicas == desiredReplicas == 7` and `ScalingLimited=False`. This is consistent with tolerance-based no-scale, but existing HPA status did not explicitly expose tolerance as the reason. |

## Example output

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

Tolerance-like no-scale:

```text
HPA default/web-tolerance
Target: Deployment/web-tolerance
Replicas: current=7 desired=7 min=2 max=10
Health score: 🟢 Healthy 100/100
Summary: HPA currently keeps the replica count unchanged.

Metrics:
  - Resource memory current=73% target=70% note="current value is above target"

Interpretation:
  - [confidence: high] desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.
  - [confidence: medium] memory metric ratio is approximately 1.043, which is close to the target.
  - [confidence: medium] This is consistent with tolerance-based no-scale. Kubernetes commonly uses a tolerance band around the target, but HPA status does not expose tolerance as an explicit reason.
  - [confidence: high] The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.
```

## Development

Prerequisites:

- Go version from [go.mod](go.mod)
- `kubectl` for cluster-backed testing
- `kind` for E2E tests
- `goreleaser` for release checks

Common commands:

```sh
make build
make test
make coverage
make lint
make release-check
```

Run E2E tests against the current kubeconfig context:

```sh
kind create cluster --name hpa-status-dev
make e2e
kind delete cluster --name hpa-status-dev
```

Release dry-run and Krew archive validation:

```sh
make krew
```

Contributor-facing design and safety notes:

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [SECURITY.md](SECURITY.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

## Findings

This POC suggests that several common HPA troubleshooting cases can be explained reasonably well using existing signals:

- metric fetch failures, through `ScalingActive=False`, condition reasons, and recent Events
- `maxReplicas` limiting, through `ScalingLimited=True`, condition reasons, and `desiredReplicas == maxReplicas`
- visible scale-up / scale-down direction, through `currentReplicas` and `desiredReplicas`
- scale-down stabilization when it is surfaced through condition reasons such as `ScaleDownStabilized`

However, it also suggests that some explanations remain difficult to provide as stable current-state output:

- which metric effectively selected the final recommendation in multi-metric HPAs, especially when later constraints such as `maxReplicas` also apply
- whether no-scale was explicitly caused by tolerance, as opposed to rounding or other conservative controller behavior
- how missing metrics or not-ready pods affected the controller's conservative recommendation
- the internal recommendation history used for stabilization

Events and human-readable condition messages are useful diagnostic hints, but this POC does not treat them as a stable structured decision record.

These results suggest that a tooling-first POC is useful before proposing new
HPA API surface. The plugin can validate how far existing signals go, while
keeping any future API discussion focused on concrete remaining ambiguities
rather than exposing the controller's full decision trace.

## Known Gaps & Future Roadmap

This plugin reports what can be inferred from existing HPA status, metrics, conditions, and events. It does not know the controller's internal intermediate calculations.
Interpretation lines are diagnostic inferences, not the HPA controller's authoritative internal decision trace. They include confidence labels so users can distinguish direct status observations from weaker interpretations. When the API does not expose a stable decision record, the output says so explicitly.

### Future Roadmap
- [x] **Integration Testing:** Added kind-based E2E tests for verification in CI.
- [x] **Visual Demos:** Added high-fidelity demo screenshots to documentation.
- [x] **Homebrew packaging:** Generate Homebrew cask metadata in a dedicated tap through GoReleaser.
- [x] **Interactive TUI Monitor:** Bubbletea-based TUI with list/detail/metrics views, filtering, sorting, and multi-select.
- [x] **Batch Analysis:** Analyze all HPAs across namespaces with `scan` and `list -A --problem`.
- [x] **Selector and multi-target workflows:** Filter `list` / `scan` with `--selector` and inspect multiple HPAs with `status hpa-a hpa-b`.
- [x] **Suggest/Fix Workflow:** Provide actionable dry-run-first patch suggestions with `--suggest` and `--fix --apply`.
- [x] **KEDA ScaledObject lookup:** `--keda` can cross-reference the matching ScaledObject when KEDA CRDs are available.
- [x] **Shell Completion:** Full tab-completion for bash/zsh/fish/powershell including flags (`--output`, `--filter`, `--sort-by`, etc.) and dynamic namespace/context completion.
- [x] **KEDA Integration Deepening:** Rich trigger display with metric name, threshold, current value, and auth ref; explicit trigger-to-HPA-metric mapping.
- [x] **Stabilization Window Countdown:** Visual countdown bar and remaining time display in TUI and text output.
- [x] **Metrics Pipeline Diagnostics:** `--diagnose-metrics` runs per-metric health checks with remediation steps.
- [x] **Resource Consistency Check:** `--check-resources` validates HPA targets against pod resource requests/limits.
- [x] **Report Output:** `--report markdown` and `--report html` generate standalone incident reports.
- [x] **Bulk Operations (TUI):** Multi-select HPAs in TUI with space/a/A keys and batch apply with s.
- [ ] **Custom Metrics Deep Dive:** Add adapter-specific context beyond visible HPA status.

## License

Apache-2.0
