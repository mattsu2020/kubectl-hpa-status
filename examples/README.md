# Examples

These manifests are practical starting points for trying `kubectl-hpa-status`
against common HPA patterns. They are intentionally small and are not used by
the automated test suite.

Apply one example:

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
```

Clean up:

```sh
kubectl delete namespace hpa-status-examples
```

| File | Purpose |
| --- | --- |
| [cpu-memory-hpa.yaml](cpu-memory-hpa.yaml) | Deployment plus CPU and memory HPA for multi-metric diagnostics. |
| [behavior-hpa.yaml](behavior-hpa.yaml) | HPA with explicit scaleUp/scaleDown behavior and stabilization windows. |
| [custom-metrics-hpa.yaml](custom-metrics-hpa.yaml) | Object metric example for clusters with a custom metrics adapter. |
| [keda-style-hpa.yaml](keda-style-hpa.yaml) | KEDA-style HPA shape that can be inspected as a normal HPA object. |

