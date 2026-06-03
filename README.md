# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mattsu2020/kubectl-hpa-status)](https://github.com/mattsu2020/kubectl-hpa-status/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/mattsu2020/kubectl-hpa-status)](https://goreportcard.com/report/github.com/mattsu2020/kubectl-hpa-status)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**Reduce HPA triage time from minutes to seconds.**

A kubectl plugin that reads existing HPA status, conditions, metrics, and events — then produces health scores, root-cause interpretation, and safe patch suggestions. No controller access required.

日本語版: [README.ja.md](README.ja.md)

---

## What problem does this solve?

`kubectl describe hpa` shows raw status fields. Operators still need to mentally correlate conditions, metrics, behavior policies, and events to answer:

- Is this HPA **healthy, capped, stabilized, or unable to read metrics**?
- Which **metric or condition** most likely explains the current behavior?
- **What should I run next**, and can I validate it safely first?

`kubectl-hpa-status` answers all three in one command.

| | `kubectl describe hpa` | `kubectl hpa status` |
|---|---|---|
| Scaling summary | Raw condition text | Operational direction with health score |
| Limitation detection | Raw min/max displayed | Auto-explains caps, suggests fixes |
| Multi-metric winner | Lists targets independently | Highlights highest-impact metric |
| Stabilization | Not tracked | Flags window + remaining time |
| Watch with diff | Requires external `watch` | Built-in refresh with delta diffs |
| Fix suggestions | None | Dry-run-first `kubectl patch` commands |

## Quick demo

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
| `tui` interactive dashboard | [images/demo.png](images/demo.png) |

## Install

### Krew (recommended)

```sh
kubectl krew install --manifest https://raw.githubusercontent.com/mattsu2020/kubectl-hpa-status/main/.krew.yaml
```

> **Note:** Krew installs as `hpa-status`, so use `kubectl hpa_status status <name>`.
> Run `kubectl plugin list` to verify.

### Homebrew

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
```

### Manual

```sh
go build -o kubectl-hpa-status . && sudo mv kubectl-hpa-status /usr/local/bin/
```

### Requirements

- **Kubernetes 1.26+** (uses `autoscaling/v2` GA API)
- kubectl configured with a kubeconfig
- metrics-server or custom/external metrics adapter

## 3 common commands

### 1. Diagnose a single HPA

```sh
kubectl hpa status web -n production --explain
```

Shows health score, root-cause interpretation, and recommended actions.

### 2. Find all problematic HPAs

```sh
kubectl hpa status list -A --problem --sort-by problem
```

Scans every namespace, highlights HPAs with issues, sorts by severity.

### 3. Apply a safe fix

```sh
kubectl hpa status web --fix --apply
```

Suggests a `kubectl patch` command, validates with server-side dry-run, shows a diff, and asks for confirmation. Persistent changes require `--dry-run=false`.

## Example output

**List view:**

```text
NAMESPACE            NAME                             CURRENT  DESIRED  HEALTH              SCORE    ISSUE                            SUMMARY
default              web                              3        5        🟢 Healthy          100                                       HPA currently wants to scale up.
default              api                              2        2        🔴 ERROR            55       ERROR: FailedGetResourceMetric   HPA cannot currently compute a scaling recommendation from metrics.
```

**Detailed status with `--explain`:**

```text
HPA default/web-multi
Target: Deployment/web-multi
Replicas: current=5 desired=5 min=2 max=5
Health score: 🔴 ScalingLimited 75/100
Summary: HPA is at maxReplicas.

Interpretation:
  - [confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.
  - [confidence: medium] Among visible resource utilization metrics, memory has the largest distance from target (ratio 1.360).

Recommended commands:
  - Raise maxReplicas: The HPA is capped at maxReplicas=5. Raising it to 10 allows the controller to add capacity if metrics still require it. (risk: medium)
    $ kubectl patch hpa web-multi -n default --type=merge -p '{"spec":{"maxReplicas":10}}'
```

## Safety model

Suggestions are intentionally conservative:

1. `--suggest` prints `kubectl patch` commands with `--dry-run=server`.
2. `--fix --apply` still defaults to dry-run and shows a field-level diff before asking for confirmation.
3. Persisting changes requires `--dry-run=false`; **this is never the default**.
4. maxReplicas suggestions include preconditions and warnings about node capacity, quotas, and cost.

## Limitations

- The Kubernetes HPA API does **not** expose the controller's internal scaling decision trace.
- Multi-metric "winner" detection is a best-effort estimate from visible `currentMetrics`.
- Tolerance, missing-metric dampening, and not-ready pod effects are not fully exposed in HPA status.
- Events are diagnostic hints, not a durable structured decision log.

## Full documentation

| Document | Content |
| --- | --- |
| [Usage Guide](docs/usage.md) | Complete flag reference, TUI key bindings, JSONPath examples, config file |
| [Troubleshooting](docs/troubleshooting.md) | Symptom→command table, FAQ, common checks |
| [Architecture](ARCHITECTURE.md) | Package layout, data flow, health score algorithm, KEDA detection design |
| [Contributing](CONTRIBUTING.md) | Development setup, testing, release process |
| [Security](SECURITY.md) | Safety model, dry-run behavior, RBAC requirements |
| [RBAC example](docs/rbac.yaml) | Minimum read-only and optional patch permissions |
| [Output schema](docs/output-schema.json) | Full JSON output schema |
| [Config example](docs/config-example.yaml) | Complete config file with all fields |

## License

Apache-2.0
