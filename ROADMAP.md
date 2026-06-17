# Roadmap

This roadmap tracks planned work that is visible to users and contributors. It is intentionally separate from the README so the README stays focused on installation and daily usage.

## Near Term

- **E2E scenario coverage:** Expand kind E2E coverage for multi-metric HPAs, behavior policies, KEDA-style external metrics, VPA conflict detection, and stabilization boundary cases.
- **README sync quality gate:** Keep `README.md` and `README.ja.md` structurally aligned through `make docs-check` and CI.
- **Remove deprecated `analyze` command:** The `analyze` (alias `diagnose`) subcommand is hidden from `--help` and scheduled for removal in v2.0. Users should migrate to `status NAME --explain`.

## Medium Term

- **Informer-based watch:** Add an opt-in informer update path for large clusters alongside the current polling mode.
- **KEP-6111 upstream adapter:** Replace the current visible-signal structured export with native upstream structured HPA decision fields when they become available.

## Recently Added

- **Durable decision recording:** `record` writes JSONL HPA snapshots and `timeline --from-record` replays them after Events expire.
- **Preflight and impact commands:** `preflight`, `behavior`, and `estimate` cover capacity validation, behavior visualization, and rough cost impact.
- **Metrics adapter probe:** `metrics probe` combines freshness, contract, adapter diagnostics, and metric hints for custom/external metrics.
- **CI/report outputs:** `lint -o github` emits GitHub Actions annotations and `scan --summary --report markdown|html` produces cluster summary reports.
- **GitOps and policy workflows:** `--export-patch`, `recommend --policy`, and `compare -A --only-drift` support PR-based operations and environment drift review.
- **Operationalization:** `alerts generate` creates starter monitoring rules and `analyze-record --detect flapping` turns durable records into churn insights.
- **Explainability and TUI safety:** `--format structured`, `explain`, score breakdowns, hidden decision factors, and in-TUI two-step batch apply preview improve operator confidence.
- **Trend and tuning workflows:** `history`, `tune`, `slo`, Prometheus query links, and carbon-aware `estimate` connect HPA behavior to incidents, SLOs, cost, and sustainability.
- **CI/CD and GitOps reporting:** `scan/list --report junit|sarif`, `list --gitops-drift`, `export --prometheus`, and local AI context packs make HPA health easier to automate and share.

## Release and Supply Chain

- Add cosign signing for release artifacts.
- Add SLSA provenance for release builds.
- Keep SBOM output in GoReleaser releases.
- Use pre-releases for experimental workflows and reserve stable releases for validated user-facing behavior.

## Community

- Label small, verifiable issues with `good first issue`.
- Keep contribution scopes explicit: target file or command, expected behavior, and validation command.
- Publish release highlights with user-facing changes, risks, and upgrade impact rather than commit hashes only.
