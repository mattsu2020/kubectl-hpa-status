# kubectl-hpa-status

POC for inspecting an HPA with the existing Kubernetes API:

```sh
kubectl hpa status <hpa-name> -n <namespace>
```

The plugin reads:

- `autoscaling/v2` `HorizontalPodAutoscaler`
- `status.currentReplicas`
- `status.desiredReplicas`
- `status.currentMetrics`
- `status.conditions`
- recent HPA Events

It intentionally does not reimplement the HPA controller's internal decision logic.

## Environment

- kind: v0.31.0
- Kubernetes server: v1.35.0
- kubectl: v1.36.1
- metrics-server: v0.8.1
- HPA API: `autoscaling/v2`

## Build

```sh
go mod tidy
go build -o kubectl-hpa-status .
```

To make it visible to `kubectl plugin list`, put the binary on `PATH`.

```sh
chmod +x ./kubectl-hpa-status
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl plugin list
```

## Validation matrix

| Case | Explainable with existing signals? | Signals used | API gap |
| --- | --- | --- | --- |
| CPU above target and scale up | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Small |
| CPU below target and scale down | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Small |
| Limited by `maxReplicas` | Yes | `ScalingLimited`, `maxReplicas` | Small |
| Metrics fetch failure | Yes | `ScalingActive=False`, Events | Small |
| Multiple metrics and final winner | Partially hard | `currentMetrics`, `spec.metrics` | Medium |
| Scale-down stabilization | Partially yes | `AbleToScale`, condition reason, Events | Medium |
| Tolerance-based no-scale | Hard | `currentMetrics`, `desiredReplicas` | Medium to large |
| Missing metrics or not-ready pods affect decision | Hard | insufficient existing status | Large |

## Local validation notes

Validated on a local kind cluster named `hpa-status-poc`.

| Case | Observed result |
| --- | --- |
| metrics-server absent | `ScalingActive=False`, `FailedGetResourceMetric`, and HPA Events explained the failure. `desiredReplicas=0` was not treated as a scale-down recommendation. |
| metrics-server present, CPU below target | `ScalingActive=True`, `currentMetrics` showed CPU below target, and desired replicas stayed unchanged. |
| CPU above target | HPA emitted `SuccessfulRescale` and scaled the deployment to `maxReplicas`. |
| maxReplicas limit | `ScalingLimited=True`, `TooManyReplicas`, and `desiredReplicas == maxReplicas` were enough to explain the visible cap. |
| scale-down stabilization | After load stopped, CPU dropped below target while `AbleToScale=True` with `ScaleDownStabilized`; the plugin could explain the visible condition but not the controller's internal recommendation history. |
| multi-metric HPA | CPU and memory both appeared in `currentMetrics`. Events and condition messages hinted that memory contributed, but per-metric replica recommendations were not exposed as structured status. |
| tolerance-like no-scale | Memory was above target (`73%/70%`) while `currentReplicas == desiredReplicas == 7` and `ScalingLimited=False`. Existing status did not expose whether tolerance, rounding, or another internal rule caused the no-scale result. |

## Example output

Multi-metric HPA:

```text
HPA default/web-multi
Target: Deployment/web-multi
Replicas: current=5 desired=5 min=2 max=5
Summary: HPA is at maxReplicas.

Metrics:
  - Resource cpu current=0% target=80% note="current value is below target"
  - Resource memory current=68% target=50% note="current value is above target"

Interpretation:
  - ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.
  - Multiple current metrics are reported, but the API does not expose per-metric replica recommendations as structured status.
  - Events and condition messages can hint at the contributing metric, but they are not a stable decision record.
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
  - desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.
  - A metric is outside its target while desiredReplicas is unchanged; this may be due to tolerance, rounding, stabilization, or conservative handling of missing metrics.
  - Existing HPA status does not expose the exact internal reason for this no-scale decision.
```

## Findings

This POC suggests that several common HPA troubleshooting cases can be explained reasonably well using existing signals:

- metric fetch failures
- `maxReplicas` limiting
- visible scale-up / scale-down direction
- scale-down stabilization when surfaced through conditions

However, it also suggests that some explanations remain difficult to provide as stable current-state output:

- which metric effectively selected the final recommendation in multi-metric HPAs
- whether no-scale was caused by tolerance versus normal rounding or other conservative controller behavior
- how missing metrics or not-ready pods affected the controller's conservative recommendation
- the internal recommendation history used for stabilization

## Limitation

This plugin reports what can be inferred from existing HPA status, metrics,
conditions, and events. It does not know the controller's internal intermediate
calculations.
