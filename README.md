# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mattsu2020/kubectl-hpa-status.svg)](https://pkg.go.dev/github.com/mattsu2020/kubectl-hpa-status)
[![Go Report Card](https://goreportcard.com/badge/github.com/mattsu2020/kubectl-hpa-status)](https://goreportcard.com/report/github.com/mattsu2020/kubectl-hpa-status)
[![Release](https://img.shields.io/github/v/release/mattsu2020/kubectl-hpa-status)](https://github.com/mattsu2020/kubectl-hpa-status/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

<details>
<summary><strong>Additional badges</strong></summary>
<br>

[![CodeQL](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml)
[![Release workflow](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml)
[![Stars](https://img.shields.io/github/stars/mattsu2020/kubectl-hpa-status?style=social)](https://github.com/mattsu2020/kubectl-hpa-status/stargazers)
[![GoReleaser](https://img.shields.io/badge/release-GoReleaser-00add8)](https://goreleaser.com/)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-blue)](https://golangci-lint.run/)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![Kubernetes](https://img.shields.io/badge/kubernetes-autoscaling%2Fv2-326ce5)](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
[![Codecov](https://codecov.io/gh/mattsu2020/kubectl-hpa-status/branch/main/graph/badge.svg)](https://codecov.io/gh/mattsu2020/kubectl-hpa-status)

</details>

![kubectl-hpa-status demo](images/demo.png)

A kubectl plugin for investigating HorizontalPodAutoscaler (HPA) status using existing Kubernetes API signals, with detailed scaling analysis.

Japanese README: [README.ja.md](README.ja.md)

> **Note**: When installed via Krew, the plugin is discovered as `kubectl hpa_status` (underscore form). The examples in this README use that form as the canonical invocation. The nested form `kubectl hpa status` (space form) also works on kubectl builds that support nested plugin discovery, but it is not universally available, so prefer `kubectl hpa_status` in scripts and runbooks.

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

## Demo

![kubectl describe hpa versus kubectl-hpa-status](images/describe-vs-hpa-status.svg)

| Workflow | Visual | Recording |
| --- | --- | --- |
| `status --explain` | [status-explain.svg](images/status-explain.svg) | [cast](docs/status-explain.cast) |
| `doctor` full diagnostics | [doctor.svg](images/doctor.svg) | [cast](docs/doctor.cast) |
| `list -A --wide --problem` | [list-wide.svg](images/list-wide.svg) | [cast](docs/list-wide.cast) |
| `scan` cluster triage | [scan-demo.svg](images/scan-demo.svg) | [cast](docs/scan.cast) |
| `timeline --since=30m` | [timeline.svg](images/timeline.svg) | [cast](docs/timeline.cast) |
| `recommend` best practice audit | [recommend.svg](images/recommend.svg) | [cast](docs/recommend.cast) |
| `--simulate-metric` what-if | [simulate.svg](images/simulate.svg) | [cast](docs/simulate.cast) |
| TUI interactive dashboard | [tui.svg](images/tui.svg) | [cast](docs/tui.cast) |
| `watch --interval 5s` | [watch-mode.svg](images/watch-mode.svg) | [cast](docs/watch.cast) |
| `--suggest` → `--fix --apply` | [apply-diff.svg](images/apply-diff.svg) | [cast](docs/fix-flow.cast) |
| Japanese labels (`--lang=ja`) | [ja-output.svg](images/ja-output.svg) | |
| JSON output | [json-output.svg](images/json-output.svg) | |
| Metrics failure | [metrics-failure.svg](images/metrics-failure.svg) | |
| Scale-down stabilization | [stabilized-output.svg](images/stabilized-output.svg) | |
| Multi-metric estimation | [multi-metric-output.svg](images/multi-metric-output.svg) | |

## Quick Start

Start from a disposable namespace and one sample HPA:

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa_status status web-multi -n hpa-status-examples --explain
kubectl hpa_status status web-multi -n hpa-status-examples --suggest
kubectl hpa_status list -n hpa-status-examples --wide
```

If you have not installed the plugin yet, replace `kubectl hpa_status` with `go run .` from this repository:

```sh
go run . status web-multi -n hpa-status-examples --explain
```

## Install

### Krew (recommended)

```sh
kubectl krew install hpa-status
```

```sh
kubectl hpa_status status <hpa-name> -n <namespace>
kubectl hpa_status list -A --wide
kubectl hpa_status <hpa-name> --suggest
```

Krew registers the plugin as `hpa-status`, discovered via `kubectl hpa_status` (underscore form). This README uses `kubectl hpa status` as the recommended form where supported. If it doesn't work, use `kubectl hpa_status status <hpa-name>` or `kubectl-hpa-status status <hpa-name>`.

### Homebrew

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

### Manual install

```sh
go build -o kubectl-hpa-status .
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl hpa status <hpa-name> -n <namespace>
```

For RBAC permissions, see [docs/rbac.yaml](docs/rbac.yaml).

### Requirements

- **Kubernetes 1.26+** (`autoscaling/v2` stable API) — officially supported and E2E-tested range. The API exists from 1.23+ and the plugin may run there, but those older versions are not part of the CI matrix. See [docs/reference.md](docs/reference.md) for the full compatibility matrix.
- kubectl configured with a kubeconfig
- metrics-server (for CPU/memory metrics) or a custom/external metrics adapter

### Status depth tiers

`status` is layered so a plain run stays fast and works under restricted RBAC. Each tier adds API reads, so pick the shallowest one that answers your question:

| Command | Reads | Use when |
| --- | --- | --- |
| `status <hpa>` | HPA only | Quick health check; RBAC-light / audited environments |
| `status <hpa> --explain` | + conditions, events, scale-target pods | "Why is it behaving this way?" |
| `status <hpa> --explain-pods` | + per-pod readiness and resource requests | Pod-level diagnosis |
| `status <hpa> --deep` | + capacity, rollout, adapter diagnostics | Full scale-out investigation |
| `status <hpa> --no-enrich` | HPA only (explicit) | Force HPA-only even if other flags are set; alias `--hpa-only` |

`--no-enrich`/`--hpa-only` and `--deep` are also available as `--analysis-profile` values (`--analysis-profile deep`). The plain `status` run reads only the HPA object, so it no longer requires Pod/Deployment permissions.

### Command surface

Commands are grouped into four layers so the top-level surface stays focused on daily HPA work:

| Layer | Commands | Notes |
| --- | --- | --- |
| Basic | `status`, `list`, `scan`, `doctor`, `watch`, `explain`, `tui` | Daily HPA inspection |
| Investigation | `trace`, `timeline`, `metrics`, `recommend`, `path`, `blockers`, `rollout`, `compare` | Root-cause analysis |
| Operational (`alpha`) | `alpha policy`, `alpha gitops`, `alpha bundle`, `alpha incident-bundle`, `alpha support-bundle` | Apply-time gating, GitOps, support data |
| Experimental (`alpha`) | `alpha capacity`, `alpha capacity-gap`, `alpha autoscaler-map`, `alpha analyze-record`, `alpha flap` | Niche tools; may change between releases |

Operational and experimental commands are available only below `alpha`; the historical top-level aliases were removed in v2.0.

## Representative Commands

```sh
# 1. Detailed status with interpretation and next steps
kubectl hpa_status status <hpa> -n <ns> --explain

# 2. Full diagnostics for a failing HPA
kubectl hpa_status doctor <hpa> -n <ns>

# 3. List all problematic HPAs across the cluster
kubectl hpa_status list -A --problem

# 4. Show concrete fix suggestions as kubectl patch commands
kubectl hpa_status status <hpa> --suggest

# 5. Cluster-wide scan for HPA issues
kubectl hpa_status scan
```

## Examples

Practical sample manifests are in [examples/](examples/).

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
```

## Multi-HPA output and partial results

`status NAME1 NAME2 ...` runs report all named HPAs together. When every HPA is reachable, the output is the per-item result in input order. When one or more HPAs fail to fetch (e.g. wrong name, wrong namespace, RBAC denial), the run no longer aborts: it emits a **partial result** so the fleet/list use case still gets the healthy items.

Exit code reflects the most severe per-item outcome:

| Per-item state | Exit code |
| --- | --- |
| All items OK | `0` |
| Any item warning (`ERROR` / `LIMITED` health), no build errors | `2` |
| Any item failed to build (fetch/build error) | `1` |

For `-o json` / `-o yaml`, multi-HPA output is wrapped in a `StatusBatch` envelope. Each item carries a `status` of `ok`, `warning`, or `error`; failed items have `status: "error"`, an `error` message, and no `report` field:

```json
{
  "apiVersion": "hpa-status/v1",
  "items": [
    {"namespace": "default", "name": "web", "status": "ok", "report": { "apiVersion": "hpa-status/v1", "analysis": { "name": "web", "health": "OK", "...": "..." } } },
    {"namespace": "default", "name": "missing", "status": "error", "error": "HPA \"missing\" was not found in namespace \"default\""}
  ]
}
```

Single-HPA `status NAME -o json` keeps the historical bare `StatusReport` shape (no envelope). Text output renders successful items normally and a single `Error: <message>` row per failed item.

## Documentation

| Document | Content |
| --- | --- |
| [Usage Guide](docs/usage.md) | Flag reference, config file, health score, TUI key bindings, JSONPath examples |
| [TUI Manual](docs/tui.md) | Interactive dashboard workflow, shortcuts, export guidance, troubleshooting |
| [Reference](docs/reference.md) | Doctor command, safe fix flow, multi-metric trace, simulator, auditor, timeline, troubleshooting |
| [Troubleshooting](docs/troubleshooting.md) | Symptom/command table and FAQ |
| [Roadmap](ROADMAP.md) | Planned TUI, metrics, KEP-6111, and supply-chain work |
| [Promotion Kit](docs/social-promotion.md) | Release announcement drafts for X, Reddit, Slack, Connpass, and Zenn |

## Community and Promotion

- Star or fork the repository if the plugin helps your HPA operations.
- Share the demo image [images/demo.png](images/demo.png) and the screenshot gallery in [images/](images/).
- Use the announcement templates in [docs/social-promotion.md](docs/social-promotion.md) when posting a release or demo.
- Open GitHub Discussions when it is enabled for questions, workflows, and troubleshooting patterns that do not fit a bug report.

## Roadmap

The active roadmap is tracked in [ROADMAP.md](ROADMAP.md). Near-term priorities are broader multi-metric/KEDA/VPA E2E coverage, KEP-6111 readiness, and continued release supply-chain hardening.

## Development

```sh
make build
make test
make coverage
make docs-check
make lint
```

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [SECURITY.md](SECURITY.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

## License

Apache-2.0
