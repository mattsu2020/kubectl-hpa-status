# Troubleshooting Patterns

Common HPA issues and how to diagnose them with kubectl-hpa-status.

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

## FAQ

**Which metric won in a multi-metric HPA?** The plugin can only estimate from
visible `currentMetrics` and `spec.metrics`. It cannot see the controller's
per-metric replica recommendations, missing-metric dampening, or final
selection before min/max and stabilization constraints.

**My HPA says `Metrics unavailable`. What should I run first?** Start with
`kubectl hpa status <name> --explain --diagnose-metrics`. For CPU and memory,
confirm `kubectl top pods` works and inspect metrics-server. For custom or
external metrics, verify the adapter `APIService`, adapter logs, and metric
selector semantics.

**How do I tell whether stabilization is the reason scale-down is delayed?**
Run `kubectl hpa status <name> --explain` or open the TUI/watch view. The
plugin highlights `ScaleDownStabilized` and stabilization window timing when it
is visible from HPA status, behavior policy, and recent events.

**Why does `kubectl hpa status` fail after Krew install?** Krew exposes
dash-separated plugin names through underscores. Run `kubectl plugin list`; if
you see `hpa-status`, use `kubectl hpa_status status <name>`.

**Why does the score say LIMITED when conditions look healthy?** Some clusters
lag or omit `ScalingLimited`; the plugin also checks explicit replica evidence
such as `current == desired == maxReplicas` and applies the smaller implicit
maxReplicas penalty.

## Common Checks

- `ScalingActive=False`: check metrics-server, custom metrics adapters, or external metrics adapters.
- `ScalingLimited=True`: check `minReplicas`, `maxReplicas`, and target utilization.
- `ScaleDownStabilized`: check `spec.behavior.scaleDown.stabilizationWindowSeconds` and wait for the stabilization window.
- Missing or stale output: compare `status.observedGeneration` with `metadata.generation`.
