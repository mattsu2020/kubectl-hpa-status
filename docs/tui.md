# TUI Manual

`kubectl-hpa-status` includes a Bubble Tea dashboard for real-time HPA triage. Use it when you want to scan many HPAs, drill into one HPA, compare metric state, or stage follow-up commands without repeatedly running separate CLI commands.

## Start the Dashboard

```sh
kubectl hpa status tui          # current namespace
kubectl hpa status tui -A       # all namespaces
kubectl hpa status tui -n prod  # one namespace
kubectl hpa status web --watch --dashboard
```

The dashboard refreshes every 5 seconds by default. Use `--interval` at startup or `+` / `-` while the TUI is running.

## Main Workflow

1. Open the dashboard with `tui` or `status <name> --watch --dashboard`.
2. Use `g` to jump to the first HPA whose health is not `OK`.
3. Press `Enter` to open the detail view.
4. Use `m`, `h`, `H`, or `T` to inspect metric diagnostics, troubleshooting hints, replica history, or replay data.
5. Use `s` for a what-if simulation before changing `minReplicas`, `maxReplicas`, or metric values.
6. Use `f` to inspect safe fix suggestions, and `d` to preview the selected fix.
7. Leave the TUI and run an explicit CLI export/apply command when you are ready to commit a GitOps change.

## Key Bindings

### Daily Triage

| Key | Action |
| --- | --- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Enter` | Open HPA detail |
| `/` | Filter by name, namespace, health, issue, or summary |
| `S` | Cycle sort: name, health-score, issue, namespace |
| `g` | Jump to first HPA whose health is not `OK` |
| `O` | Open cluster overview from the list |

### Refresh and Display

| Key | Action |
| --- | --- |
| `r` | Refresh data now |
| `p` | Pause or resume auto-refresh |
| `+` / `=` | Decrease refresh interval, minimum 1s |
| `-` | Increase refresh interval, maximum 60s |
| `Esc` | Go back to the previous dashboard level |
| `?` | Open or close in-TUI help |
| `q` / `Ctrl+c` | Quit |

### Detail Drill-Down

| Key | Action |
| --- | --- |
| `m` | Open per-metric diagnostics |
| `s` | Open the what-if simulation panel |
| `M` | Toggle parameter and metric-value simulation inside simulation |
| `Tab` | Move to the next simulation field |
| `Shift+Tab` | Move to the previous simulation field |
| `f` | Open the fix wizard when suggestions are available |
| `d` | Preview the selected fix without applying it |
| `T` | Open replay timeline from `hpa-trace.json` |
| `H` | Open history and sparkline view |
| `h` | Open metric hints troubleshooting when hints are available |

### Selection and Batch Work

| Key | Action |
| --- | --- |
| `space` | Toggle current HPA selection |
| `a` | Select all visible HPAs |
| `A` | Clear the selection |
| `B` | Run the batch auditor on selected HPAs |
| `x` | Show batch apply guidance for selected HPAs |

## Views

| View | Opens From | Purpose |
| --- | --- | --- |
| List | startup | Scan health, replica counts, stabilization state, trend, issue, and summary |
| Detail | `Enter` | Inspect one HPA, including conditions, suggestions, KEDA/VPA context, and diagnostics |
| Metrics | `m` | Review per-metric status and remediation steps |
| Simulation | `s` | Compare before/after outcomes for HPA parameters or metric values |
| Fix wizard | `f` | Review suggested patches or commands and dry-run previews |
| Replay | `T` | Load `hpa-trace.json` and inspect timeline snapshots |
| History | `H` | Inspect recent desired replica history and sparklines |
| Metric hints | `h` | Follow step-by-step metric troubleshooting commands |
| Overview | `O` | Summarize cluster-wide health from the list view |
| Batch auditor | `B` | Audit selected HPAs together |

## Export and GitOps Workflow

The TUI intentionally keeps export operations explicit so generated changes are reviewable outside the full-screen dashboard. Use the TUI to identify and inspect the HPA, then run one of these commands:

```sh
kubectl hpa status <name> -n <namespace> --suggest --export yaml
kubectl hpa status <name> -n <namespace> --suggest --export kustomize
kubectl hpa status <name> -n <namespace> --suggest --export helm-values
```

For multi-HPA work, select HPAs in the TUI, run `B` to audit them, then export or apply changes one HPA at a time through the CLI. This avoids hiding generated patches inside an interactive session and fits GitOps review flows.

## Troubleshooting

| Symptom | Action |
| --- | --- |
| No HPAs appear | Check namespace selection, `-A`, RBAC, and `kubectl auth can-i list hpa` |
| Metrics view is incomplete | Verify Metrics API and use metric hints with `h` when available |
| Replay view is empty | Ensure `hpa-trace.json` exists in the current working directory |
| Simulation says no parameters changed | Enter a value different from the current field value |
| Fix wizard is unavailable | Run detail view on an HPA with suggestions, or use `--suggest` in CLI output |
