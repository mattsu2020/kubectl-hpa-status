# Usage Guide

Detailed usage instructions for kubectl-hpa-status.

## Command Reference

```sh
kubectl hpa status <hpa-name> [<hpa-name>...] [-n namespace] [--context context] [--events=false]
kubectl hpa status doctor <hpa-name> -n <namespace>
kubectl hpa status timeline <hpa-name> --since=30m
kubectl hpa status record -A --interval=15s --duration=1h -o hpa-history.jsonl
kubectl hpa status timeline <hpa-name> --from-record hpa-history.jsonl
kubectl hpa status preflight <hpa-name> --raise-max 20
kubectl hpa status metrics probe <hpa-name>
kubectl hpa status behavior <hpa-name>
kubectl hpa status estimate <hpa-name> --max-replicas 30 --pod-cost 0.12
kubectl hpa status compare -A --from-context stg --to-context prod --only-drift
kubectl hpa status alerts generate --format prometheus
kubectl hpa status analyze-record hpa-history.jsonl --detect flapping
kubectl hpa status <hpa-name> --watch --interval 5s
kubectl hpa status <hpa-name> --watch --timeout 2m --until-condition scaling-limited
kubectl hpa status list [-A] [--selector app=web] [--sort-by desired] [--filter scaling-limited]
kubectl hpa status list -A --problem
kubectl hpa status scan --selector app=web
kubectl hpa status ls [-A] --wide
kubectl hpa status watch <hpa-name> --interval 5s
```

Direct binary usage is also supported:

```sh
kubectl-hpa-status status <hpa-name> -n <namespace>
kubectl-hpa-status doctor <hpa-name> -n <namespace>
kubectl-hpa-status timeline <hpa-name> --since=30m
kubectl-hpa-status lint -f k8s/ -o github
kubectl-hpa-status status <hpa-name> --suggest
kubectl-hpa-status status <hpa-name> --fix --apply
kubectl-hpa-status scan
kubectl-hpa-status list -A
kubectl-hpa-status completion zsh
```

## Flag Reference

| Flag | Applies to | Description |
| --- | --- | --- |
| `-n, --namespace` | all commands | Namespace to read when `-A` is not set. Defaults to the current kubeconfig namespace or `default`. |
| `-A, --all-namespaces` | `list`, `scan`, completion | List HPAs across all namespaces. |
| `-l, --selector` | `list`, `scan` | Kubernetes label selector passed to the HPA list call, such as `app=web,tier!=canary`. |
| `--context`, `--kubeconfig`, `--cluster` | all commands | Explicit kubeconfig selection. |
| `--config` | all commands | Read defaults from a YAML/JSON config file. Defaults to `~/.kube/hpa-status.yaml` when present. |
| `--chunk-size` | `list`, `scan`, `tui` | Kubernetes list page size. Defaults to 500; set 0 to disable pagination. |
| `--health-weight name=value` | all analysis commands | Override one health score penalty from the CLI. Repeatable; names include `scalingInactive`, `unableToScale`, `scalingLimited`, `implicitMaxReplicas`, `scaleDownStabilized`, and `atMinimumReplicas`. |
| `-o table\|wide\|json\|yaml\|jsonpath=...\|template=...` | status, doctor, analyze, list, scan | Output format. YAML is supported for both single and multiple HPA output. For the JSON schema, see [output-schema.json](output-schema.json). |
| `--wide` | table output | Show target, min, max, and replica delta columns where applicable. |
| `--sort-by namespace\|name\|current\|desired\|diff\|health-score\|issue\|problem` | `list`, `scan` | Sort list output. `problem` puts the lowest health score and largest replica delta first. |
| `--filter all\|ok\|error\|limited\|scaling-limited\|issue` | `list`, `scan` | Filter by health or issue text. |
| `--health-score`, `--max-score` | `list`, `scan` | Show only HPAs whose health score is at or below the threshold. |
| `--min-score` | `list`, `scan` | Show only HPAs whose health score is at or above the threshold. |
| `--problem` | `list`, `scan` | Show only HPAs with a visible issue. |
| `--color auto\|always\|never` | text output | Control terminal color output. |
| `--interpret` | `status` | Include diagnostic interpretation in compact status output. |
| `--explain` | `status`, `doctor`, `analyze` | Include detailed interpretation and recommended actions. `doctor` enables this by default. |
| `--suggest`, `--recommend` | `status`, `doctor`, `analyze` | Include concrete `kubectl patch` commands when a safe HPA spec suggestion is visible. `--recommend` is an alias for `--suggest`. |
| `--export-patch yaml\|kustomize\|helm-values\|directory` | `status`, `scan` | Export safe suggestions as GitOps-friendly patches. `directory` is for `scan`/`list` and writes per-HPA YAML files. |
| `--fix` | `status`, `doctor`, `analyze` | Show a stronger fix plan with applicable patches. |
| `--diff` | `status`, `doctor`, `analyze` | Include field-level diffs for suggested HPA spec patches. |
| `--apply` | `status`, `doctor`, `analyze`, `list`, `scan` | Validate suggested HPA patches with server-side dry-run by default. For `list`, combine it with `--problem`, `--filter`, or a score filter. |
| `--dry-run=false` | `--apply` workflow | Persist changes; still shows a diff and asks for confirmation unless `-y` is set. |
| `--keda` | `status`, `doctor`, `analyze` | For KEDA-managed HPAs, look up the matching ScaledObject and include trigger details (metric name, threshold, current value, auth ref). |
| `--vpa` | `status`, `doctor`, `analyze` | Detect VerticalPodAutoscaler conflicts with the HPA target. |
| `--diagnose-metrics` | `status`, `doctor`, `analyze` | Run comprehensive metrics pipeline health checks with per-metric status and remediation steps. `doctor` enables this by default. |
| `--metrics-freshness` | `status`, `doctor`, `analyze` | Check currentMetrics presence, FailedGet*Metric Events, metrics API discovery, resource PodMetrics sample timestamp/window, and KEDA trigger context. `doctor` enables this by default. |
| `--check-resources` | `status`, `doctor`, `analyze` | Validate HPA target utilization against pod resource requests/limits. `doctor` enables this by default. |
| `--report markdown\|html` | `status`, `doctor`, `list` | Generate standalone reports in Markdown or HTML format. |
| `--summary` | `scan` with `--report markdown\|html` | Include cluster summary, worst HPAs, and prioritized actions. |
| `--lang=ja`, `-o ja` | text output | Show Japanese text labels. |
| `--no-interpret` | `status`, `doctor`, `analyze` | Omit interpretation and show status-derived data only. |
| `--events=false` | `status`, `doctor`, `analyze` | Omit recent HPA Events. |
| `--events=3` | `status`, `doctor`, `analyze` | Show the latest 3 HPA Events. |
| `--watch --interval 5s` | `status`, `watch` | Refresh one HPA periodically. Watch mode accepts exactly one HPA name. |
| `--dashboard` | `watch` | Render watch output as a compact terminal dashboard. |
| `--timeout 2m` | watch mode | Stop watch after a duration. |
| `--until-condition scaling-limited` | watch mode | Stop watch once the normalized condition type is present. |
| `--since=30m` | `timeline` | Reconstruct an HPA decision timeline from recent Kubernetes Events. Supports `30m`, `1h`, etc. |
| `--from-record FILE` | `timeline` | Read durable JSONL/JSON snapshots written by `record` instead of relying on Kubernetes Events. |
| `--output-file FILE` | `record` | Write durable HPA decision snapshots to JSONL. `record` also accepts `-o FILE` as a convenience. |
| `--raise-max N` | `preflight` | Validate quota and capacity before raising `maxReplicas` to `N`. |
| `--hidden-factors` | `status`, `doctor` | Show partially visible controller factors such as missing metrics, tolerance, not-yet-ready pods, and stabilization. |
| `--node-autoscaler`, `--karpenter` | `status`, `doctor`, `capacity` | Include node provisioning and scheduler/capacity context for scale-out failures. |
| `--max-replicas N` | `estimate` | Proposed `maxReplicas` for cost and availability impact estimation. |
| `--pod-cost N` | `estimate` | Optional per-pod hourly cost used by the cost estimate. |
| `--qps` | all commands | Client-side rate limiting queries per second (0 uses client-go default). |
| `--burst` | all commands | Client-side rate limiting burst size (0 uses client-go default). |
| `--version` | root | Print the plugin version. |

## Health Score

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

## Config File

If `--config` is omitted and `~/.kube/hpa-status.yaml` exists, the plugin reads it as default CLI settings. Explicit flags always win over config values.

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

For a complete example with all fields, see [config-example.yaml](config-example.yaml).

## Interactive TUI

Launch a real-time interactive dashboard for monitoring HPAs across the cluster:

```sh
kubectl hpa status tui          # current namespace
kubectl hpa status tui -A       # all namespaces
kubectl hpa status web --watch --dashboard
```

For the complete workflow, view descriptions, export guidance, and troubleshooting notes, see [TUI Manual](tui.md).

Common key bindings:

| Key | Action |
| --- | --- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Enter` | Open HPA detail view |
| `Esc` | Go back / Close help |
| `/` | Filter by name, namespace, health status, or issue text |
| `S` | Cycle sort: name, health-score, issue, namespace |
| `g` | Jump to first problematic HPA (health ≠ OK) |
| `O` | Open cluster overview from the list |
| `m` | View per-metric diagnostics detail |
| `s` | Open the what-if simulation panel |
| `M` | Toggle parameter and metric-value simulation inside simulation |
| `Tab` / `Shift+Tab` | Move between simulation fields |
| `f` | Open the fix wizard when suggestions are available |
| `d` | Preview the selected fix without applying it |
| `T` | Open replay timeline from `hpa-trace.json` |
| `H` | Open history and sparkline view |
| `h` | Open metric hints troubleshooting when hints are available |
| `+` / `=` | Decrease refresh interval (faster) |
| `-` | Increase refresh interval (slower) |
| `space` | Toggle HPA selection for batch operations |
| `a` | Select all filtered HPAs |
| `A` | Deselect all |
| `B` | Run the batch auditor on selected HPAs |
| `x` | Show batch apply guidance for selected HPAs |
| `r` | Refresh data now |
| `p` | Pause / resume auto-refresh |
| `?` | Toggle key binding help overlay |
| `q` / `Ctrl+c` | Quit |

The dashboard auto-refreshes every 5 seconds by default. Use `+`/`-` keys to adjust at runtime (1s–60s). Filter accepts partial matches across multiple fields. Sort cycles through available columns. Use `g` to quickly jump to the first HPA that needs attention. Export remains an explicit CLI workflow; use `--suggest --export yaml`, `--export kustomize`, or `--export helm-values` after inspecting an HPA in the TUI.

## JSONPath and Template Output

```sh
# List HPA names with health scores
kubectl hpa status list -A -o jsonpath='{range .items[*]}{.namespace}/{.name} {.healthScore}{"\n"}{end}'

# Show only HPAs with health score below 80
kubectl hpa status list -A -o jsonpath='{range .items[?(@.healthScore<80)]}{.namespace}/{.name} {.health}{"\n"}{end}'

# Extract KEDA ScaledObject name for KEDA-managed HPAs
kubectl hpa status <hpa> -o jsonpath='{.analysis.keda.scaledObjectName}'

# Get VPA conflict warning
kubectl hpa status <hpa> --vpa -o jsonpath='{.analysis.vpaConflict.warning}'

# Get structured interpretation entries
kubectl hpa status <hpa> -o jsonpath='{range .analysis.structuredInterpretation[*]}{.severity} {.text}{"\n"}{end}'

# Output per-HPA summary as JSON for automation
kubectl hpa status list -A -o json | jq '.items[] | {name, namespace, healthScore, issue}'
```

For the full JSON schema, see [output-schema.json](output-schema.json).

## Output Interpretation

- `Summary` is the visible state derived from HPA status.
- `Recommended actions` are operational hints based on visible conditions and behavior settings.
- `Interpretation` is diagnostic inference, not the controller's private decision trace.

Every interpretation line carries a classification label:

- `[observed]` — directly read from HPA status fields (conditions, replicas, metrics).
- `[estimated]` — inferred from visible signals but not directly confirmable via the API.
- `[unknown]` — the Kubernetes HPA controller does not expose this information (e.g., missing-metric dampening, not-ready pod adjustments).

Multi-metric "winner" lines are intentionally labeled `[estimated]`. Kubernetes HPA status does not expose per-metric replica recommendations, so the plugin highlights the metric with the largest visible distance from target. For the full rationale, see [Reference — Decision Confidence Model](reference.md#decision-confidence-model).

## Examples

Practical manifests live in [examples/](../examples/):

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
kubectl hpa status web-multi -n hpa-status-examples --diagnose-metrics
kubectl hpa status web-multi -n hpa-status-examples --check-resources
kubectl hpa status web-multi -n hpa-status-examples --report markdown
kubectl delete namespace hpa-status-examples
```
