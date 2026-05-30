# kubectl-hpa-status

A kubectl plugin for inspecting HorizontalPodAutoscaler status with detailed
scaling analysis using existing Kubernetes API signals.

## Quick usage

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status <hpa-name> --explain
kubectl hpa status list -A --wide
kubectl hpa status ls -A -o json
kubectl hpa status <hpa-name> -o 'jsonpath={.analysis.summary}'
```

How to read the output:

- `Summary` is the visible state derived from HPA status.
- `Recommended actions` are operational hints based on visible conditions and behavior settings.
- `Interpretation` is diagnostic inference, not the controller's private decision trace.
- `confidence: high` means the line is based on explicit status fields; `confidence: medium` means the status is consistent with the explanation but the API does not expose the exact internal reason.

Common troubleshooting checks:

- `ScalingActive=False`: check metrics-server, custom metrics adapters, or external metrics adapters.
- `ScalingLimited=True`: check `minReplicas`, `maxReplicas`, and target utilization.
- `ScaleDownStabilized`: check `spec.behavior.scaleDown.stabilizationWindowSeconds` and wait for the stabilization window.
- missing or stale output: compare `status.observedGeneration` with `metadata.generation`.

## Install

### Krew (recommended)

```sh
kubectl krew install hpa-status
kubectl hpa status <hpa-name> -n <namespace>
```

Krew installs the plugin as `hpa-status`. For plugins whose names contain
dashes, Krew creates a kubectl-visible symlink using underscores, so
`hpa-status` is discoverable by kubectl as the nested command
`kubectl hpa status`. Depending on kubectl plugin discovery behavior,
`kubectl hpa-status status <hpa-name>` may also work.

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

The Go module path currently follows the repository import path
`github.com/mattsu2020/kubehpa_cli`. The released plugin and binary keep the
user-facing name `kubectl-hpa-status`. Keep `go.mod`, import paths, the GitHub
repository owner/name, and `.goreleaser.yml` ldflags in one rename change if the
repository moves to `kubectl-hpa-status` or an organization account.

## Usage

```sh
kubectl hpa status <hpa-name> [-n namespace] [--context context] [--events=false]
kubectl hpa status <hpa-name> --watch --interval 5s
kubectl hpa status analyze <hpa-name>
kubectl hpa status list [-A] [-o table|wide|json|yaml]
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
kubectl-hpa-status list -A
kubectl-hpa-status completion zsh
```

`analyze` is the detailed diagnostic command. `status` is intentionally more
compact by default; pass `--interpret` when you want interpretation in status
output.

Common flags:

- `-n, --namespace`: namespace
- `-A, --all-namespaces`: list HPAs across all namespaces
- `--context`, `--kubeconfig`, `--cluster`: explicit kubeconfig selection
- `-o table|wide|json|yaml|jsonpath=...|template=...`: output format
- `--wide`: show target, min, and max columns in table output
- `--interpret`: include diagnostic interpretation in compact status output
- `--explain`: include detailed interpretation and recommended actions
- `--no-interpret`: omit interpretation and show status-derived data only
- `--events=false`: omit recent Events
- `--events=3`: show the latest 3 HPA Events
- `--watch --interval 5s`: refresh one HPA from the main command
- `--version`: print the plugin version

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

## Validated environment

- kind: v0.31.0
- kind node image: `kindest/node:v1.35.0`
- Kubernetes server: v1.35.0
- kubectl: v1.36.1
- metrics-server: v0.8.1
- HPA API: `autoscaling/v2`

metrics-server was installed from the upstream release manifest with the
kind-specific `--kubelet-insecure-tls` option.

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
NAMESPACE            NAME                             CURRENT  DESIRED  HEALTH     ISSUE                            SUMMARY
default              web                              3        5        OK                                          HPA currently wants to scale up.
default              api                              2        2        ! ERROR    ERROR: FailedGetResourceMetric   HPA cannot currently compute a scaling recommendation from metrics.
```

Multi-metric HPA:

```text
HPA default/web-multi
Target: Deployment/web-multi
Replicas: current=5 desired=5 min=2 max=5
Summary: HPA is at maxReplicas.

Metrics:
  - Resource cpu current=0% target=80% note="current value is below target"
  - Resource memory current=68% target=50% note="current value is above target"

Recommended actions:
  - HPA is capped at maxReplicas; raise maxReplicas or reduce load/target utilization if more capacity is expected.

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
Summary: HPA currently keeps the replica count unchanged.

Metrics:
  - Resource memory current=73% target=70% note="current value is above target"

Interpretation:
  - [confidence: high] desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.
  - [confidence: medium] memory metric ratio is approximately 1.043, which is close to the target.
  - [confidence: medium] This is consistent with tolerance-based no-scale. Kubernetes commonly uses a tolerance band around the target, but HPA status does not expose tolerance as an explicit reason.
  - [confidence: high] The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.
```

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

## Limitation

This plugin reports what can be inferred from existing HPA status, metrics,
conditions, and events. It does not know the controller's internal intermediate
calculations.

Interpretation lines are diagnostic inferences, not the HPA controller's
authoritative internal decision trace. They include confidence labels so users
can distinguish direct status observations from weaker interpretations. When
the API does not expose a stable decision record, the output says so explicitly.

## Development roadmap

The repository is now structured for incremental feature work:

- `cmd/`: Cobra command wiring and CLI output selection
- `internal/kube/`: kubeconfig and Kubernetes client construction
- `pkg/hpa/`: HPA status analysis, formatting, and unit-tested interpretation logic

Near-term work:

- add integration tests with kind/testenv for real HPA behavior
- expand `list` issue detection beyond conditions and replica limits
- add richer structured output contracts as the CLI stabilizes
- add screenshots or terminal recordings once the output format is settled

## License

Apache-2.0
