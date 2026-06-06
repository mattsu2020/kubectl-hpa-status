# Usage Guide

Detailed usage instructions for kubectl-hpa-status.

## Command Reference

```sh
kubectl hpa status <hpa-name> [<hpa-name>...] [-n namespace] [--context context] [--events=false]
kubectl hpa status doctor <hpa-name> -n <namespace>
kubectl hpa status timeline <hpa-name> --since=30m
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
| `--lang=ja`, `-o ja` | text output | Show Japanese text labels. |
| `--no-interpret` | `status`, `doctor`, `analyze` | Omit interpretation and show status-derived data only. |
| `--events=false` | `status`, `doctor`, `analyze` | Omit recent HPA Events. |
| `--events=3` | `status`, `doctor`, `analyze` | Show the latest 3 HPA Events. |
| `--watch --interval 5s` | `status`, `watch` | Refresh one HPA periodically. Watch mode accepts exactly one HPA name. |
| `--dashboard` | `watch` | Render watch output as a compact terminal dashboard. |
| `--timeout 2m` | watch mode | Stop watch after a duration. |
| `--until-condition scaling-limited` | watch mode | Stop watch once the normalized condition type is present. |
| `--since=30m` | `timeline` | Reconstruct an HPA decision timeline from recent Kubernetes Events. Supports `30m`, `1h`, etc. |
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
| `+` / `=` | Decrease refresh interval (faster) |
| `-` | Increase refresh interval (slower) |
| `space` | Toggle HPA selection for batch operations |
| `a` | Select all filtered HPAs |
| `A` | Deselect all |
| `s` | Show apply hint for selected HPAs |
| `r` | Refresh data now |
| `p` | Pause / resume auto-refresh |
| `?` | Toggle key binding help overlay |
| `q` / `Ctrl+c` | Quit |

The dashboard auto-refreshes every 5 seconds by default. Use `+`/`-` keys to adjust at runtime (1s–60s). Filter accepts partial matches across multiple fields. Sort cycles through available columns. Use `g` to quickly jump to the first HPA that needs attention.

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
- `confidence: high` means the line is based on explicit status fields; `confidence: medium` means the status is consistent with the explanation but the API does not expose the exact internal reason.
- Multi-metric "winner" lines are intentionally labeled as estimates. Kubernetes HPA status does not expose per-metric replica recommendations today, so the plugin highlights the metric with the largest visible distance from target instead of claiming the exact controller winner.

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
